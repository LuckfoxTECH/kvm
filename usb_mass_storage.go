package kvm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"github.com/psanford/httpreadat"

	"kvm/resource"
)

func writeFile(path string, data string) error {
	return os.WriteFile(path, []byte(data), 0644)
}

func getMassStorageImage() (string, error) {
	massStorageFunctionPath, err := gadget.GetPath("mass_storage_lun0")
	if err != nil {
		return "", fmt.Errorf("failed to get mass storage path: %w", err)
	}

	imagePath, err := os.ReadFile(path.Join(massStorageFunctionPath, "file"))
	if err != nil {
		return "", fmt.Errorf("failed to get mass storage image path: %w", err)
	}
	return strings.TrimSpace(string(imagePath)), nil
}

func setMassStorageImage(imagePath string) error {
	massStorageFunctionPath, err := gadget.GetPath("mass_storage_lun0")
	if err != nil {
		return fmt.Errorf("failed to get mass storage path: %w", err)
	}

	if err := writeFile(path.Join(massStorageFunctionPath, "file"), imagePath); err != nil {
		return fmt.Errorf("failed to set image path: %w", err)
	}
	return nil
}

func setMassStorageMode(cdrom bool) error {
	mode := "0"
	if cdrom {
		mode = "1"
	}

	err, changed := gadget.OverrideGadgetConfig("mass_storage_lun0", "cdrom", mode)
	if err != nil {
		return fmt.Errorf("failed to set cdrom mode: %w", err)
	}

	if !changed {
		return nil
	}

	return gadget.UpdateGadgetConfig()
}

func onDiskMessage(msg webrtc.DataChannelMessage) {
	logger.Info().Int("len", len(msg.Data)).Msg("Disk Message")
	diskReadChan <- msg.Data
}

func mountImage(imagePath string) error {
	err := setMassStorageImage("")
	if err != nil {
		return fmt.Errorf("remove mass storage image error: %w", err)
	}
	err = setMassStorageImage(imagePath)
	if err != nil {
		return fmt.Errorf("set mass storage image error: %w", err)
	}
	err = setMassStorageImage(imagePath)
	if err != nil {
		return fmt.Errorf("set Mass Storage Image Error: %w", err)
	}
	return nil
}

var nbdDevice *NBDDevice

const imagesFolder = "/userdata/picokvm/share"
const SDImagesFolder = "/mnt/sdcard"

func initImagesFolder() error {
	err := os.MkdirAll(imagesFolder, 0755)
	if err != nil {
		return fmt.Errorf("failed to create images folder: %w", err)
	}
	return nil
}

func rpcMountBuiltInImage(filename string) error {
	logger.Info().Str("filename", filename).Msg("Mount Built-In Image")
	if err := initImagesFolder(); err != nil {
		return err
	}

	imagePath := filepath.Join(imagesFolder, filename)

	// Check if the file exists in the imagesFolder
	if _, err := os.Stat(imagePath); err == nil {
		return mountImage(imagePath)
	}

	// If not, try to find it in ResourceFS
	file, err := resource.ResourceFS.Open(filename)
	if err != nil {
		return fmt.Errorf("image %s not found in built-in resources: %w", filename, err)
	}
	defer file.Close()

	// Create the file in imagesFolder
	outFile, err := os.Create(imagePath)
	if err != nil {
		return fmt.Errorf("failed to create image file: %w", err)
	}
	defer outFile.Close()

	// Copy the content
	_, err = io.Copy(outFile, file)
	if err != nil {
		return fmt.Errorf("failed to write image file: %w", err)
	}

	// Mount the newly created image
	return mountImage(imagePath)
}

func getMassStorageCDROMEnabled() (bool, error) {
	massStorageFunctionPath, err := gadget.GetPath("mass_storage_lun0")
	if err != nil {
		return false, fmt.Errorf("failed to get mass storage path: %w", err)
	}
	data, err := os.ReadFile(path.Join(massStorageFunctionPath, "cdrom"))
	if err != nil {
		return false, fmt.Errorf("failed to read cdrom mode: %w", err)
	}
	// Trim any whitespace characters. It has a newline at the end
	trimmedData := strings.TrimSpace(string(data))
	return trimmedData == "1", nil
}

type VirtualMediaUrlInfo struct {
	Usable bool
	Reason string //only populated if Usable is false
	Size   int64
}

func rpcCheckMountUrl(url string) (*VirtualMediaUrlInfo, error) {
	return nil, errors.New("not implemented")
}

type VirtualMediaSource string

const (
	WebRTC    VirtualMediaSource = "WebRTC"
	HTTP      VirtualMediaSource = "HTTP"
	Storage   VirtualMediaSource = "Storage"
	SDStorage VirtualMediaSource = "SDStorage"
)

type VirtualMediaMode string

const (
	CDROM VirtualMediaMode = "CDROM"
	Disk  VirtualMediaMode = "Disk"
)

type VirtualMediaState struct {
	Source   VirtualMediaSource `json:"source"`
	Mode     VirtualMediaMode   `json:"mode"`
	Filename string             `json:"filename,omitempty"`
	URL      string             `json:"url,omitempty"`
	Path     string             `json:"path,omitempty"`
	Size     int64              `json:"size"`
}

var currentVirtualMediaState *VirtualMediaState
var virtualMediaStateMutex sync.RWMutex

func rpcGetVirtualMediaState() (*VirtualMediaState, error) {
	virtualMediaStateMutex.RLock()
	defer virtualMediaStateMutex.RUnlock()
	return currentVirtualMediaState, nil
}

func unmountImageCore() (*VirtualMediaState, error) {
	virtualMediaStateMutex.Lock()
	defer virtualMediaStateMutex.Unlock()

	unmountedState := currentVirtualMediaState

	var err error
	for i := 0; i < 3; i++ {
		err = setMassStorageImage("\n")
		if err == nil {
			break
		}
		logger.Warn().Err(err).Int("attempt", i+1).Msg("unmount attempt failed, retrying...")
		time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
	}
	if err != nil {
		logger.Warn().Err(err).Msg("Remove Mass Storage Image Error after retries")
		return unmountedState, fmt.Errorf("device or resource busy")
	}

	time.Sleep(500 * time.Millisecond)
	if nbdDevice != nil {
		nbdDevice.Close()
		nbdDevice = nil
	}
	currentVirtualMediaState = nil
	clearPersistedVirtualMediaState()
	return unmountedState, nil
}

func rpcUnmountImage() error {
	unmountedState, err := unmountImageCore()
	if err != nil {
		return err
	}
	clearAutoMountConfigIfMatch(unmountedState)
	return nil
}

// clearAutoMountConfigIfMatch clears the auto-mount config when the unmounted
// file was set for auto-mount, so an explicit unmount keeps it unmounted.
func clearAutoMountConfigIfMatch(state *VirtualMediaState) {
	if state == nil || state.Filename == "" {
		return
	}

	var autoMountSource string
	switch state.Source {
	case Storage:
		autoMountSource = "kvm"
	case SDStorage:
		autoMountSource = "sd"
	default:
		return
	}

	if config.AutoMountImage == nil || config.AutoMountImage.Filename != state.Filename || config.AutoMountImage.Source != autoMountSource {
		return
	}

	config.AutoMountImage = nil
	if err := SaveConfig(); err != nil {
		logger.Error().Err(err).Msg("failed to clear auto-mount config after unmount")
	} else {
		logger.Info().Str("filename", state.Filename).Msg("cleared auto-mount config after unmount")
	}
}

var httpRangeReader *httpreadat.RangeReader

func getInitialVirtualMediaState() (*VirtualMediaState, error) {
	cdromEnabled, err := getMassStorageCDROMEnabled()
	if err != nil {
		return nil, fmt.Errorf("failed to get mass storage cdrom enabled: %w", err)
	}

	diskPath, err := getMassStorageImage()
	if err != nil {
		return nil, fmt.Errorf("failed to get mass storage image: %w", err)
	}

	initialState := &VirtualMediaState{
		Source: Storage,
		Mode:   Disk,
	}

	if cdromEnabled {
		initialState.Mode = CDROM
	}

	// TODO: check if it's WebRTC or HTTP
	switch diskPath {
	case "":
		return nil, nil
	case "/dev/nbd0":
		initialState.Source = HTTP
		initialState.URL = "/"
		initialState.Size = 1
	default:
		initialState.Filename = filepath.Base(diskPath)
		// get size from file
		logger.Info().Str("diskPath", diskPath).Msg("getting file size")
		info, err := os.Stat(diskPath)
		if err != nil {
			return nil, fmt.Errorf("[rpcGetInitialVirtualMediaState]failed to get file info: %w", err)
		}
		initialState.Size = info.Size()
	}

	return initialState, nil
}

func setInitialVirtualMediaState() error {
	virtualMediaStateMutex.Lock()
	defer virtualMediaStateMutex.Unlock()
	initialState, err := getInitialVirtualMediaState()
	if err != nil {
		return fmt.Errorf("failed to get initial virtual media state: %w", err)
	}
	currentVirtualMediaState = initialState

	logger.Info().Interface("initial_virtual_media_state", initialState).Msg("initial virtual media state set")
	return nil
}

func persistVirtualMediaState(state *VirtualMediaState) {
	config.PersistedVirtualMediaState = state
	if err := SaveConfig(); err != nil {
		logger.Error().Err(err).Msg("failed to persist virtual media state")
	}
}

func clearPersistedVirtualMediaState() {
	config.PersistedVirtualMediaState = nil
	if err := SaveConfig(); err != nil {
		logger.Error().Err(err).Msg("failed to clear persisted virtual media state")
	}
}

func remountPersistedVirtualMediaState() {
	if config.PersistedVirtualMediaState == nil {
		return
	}

	state := config.PersistedVirtualMediaState
	logger.Info().Interface("state", state).Msg("attempting to remount persisted virtual media")

	var err error
	switch state.Source {
	case Storage:
		err = rpcMountWithStorage(state.Filename, state.Mode)
	case SDStorage:
		err = rpcMountWithSDStorage(state.Filename, state.Mode)
	default:
		logger.Warn().Str("source", string(state.Source)).Msg("unsupported source for auto-remount, clearing state")
		clearPersistedVirtualMediaState()
		return
	}

	if err != nil {
		logger.Error().Err(err).Msg("failed to remount persisted virtual media, clearing state")
		clearPersistedVirtualMediaState()
	}
}

func rpcMountWithHTTP(url string, mode VirtualMediaMode) error {
	virtualMediaStateMutex.Lock()
	if currentVirtualMediaState != nil {
		virtualMediaStateMutex.Unlock()
		return fmt.Errorf("another virtual media is already mounted")
	}
	httpRangeReader = httpreadat.New(url)
	n, err := httpRangeReader.Size()
	if err != nil {
		virtualMediaStateMutex.Unlock()
		return fmt.Errorf("failed to use http url: %w", err)
	}
	logger.Info().Str("url", url).Int64("size", n).Msg("using remote url")

	if err := setMassStorageMode(mode == CDROM); err != nil {
		return fmt.Errorf("failed to set mass storage mode: %w", err)
	}

	currentVirtualMediaState = &VirtualMediaState{
		Source: HTTP,
		Mode:   mode,
		URL:    url,
		Size:   n,
	}
	virtualMediaStateMutex.Unlock()

	logger.Debug().Msg("Starting nbd device")
	nbdDevice = NewNBDDevice()
	err = nbdDevice.Start()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to start nbd device")
		return err
	}
	logger.Debug().Msg("nbd device started")
	//TODO: replace by polling on block device having right size
	time.Sleep(1 * time.Second)
	err = setMassStorageImage("/dev/nbd0")
	if err != nil {
		return err
	}
	logger.Info().Msg("usb mass storage mounted")
	return nil
}

func rpcMountWithWebRTC(filename string, size int64, mode VirtualMediaMode) error {
	virtualMediaStateMutex.Lock()
	if currentVirtualMediaState != nil {
		virtualMediaStateMutex.Unlock()
		return fmt.Errorf("another virtual media is already mounted")
	}
	currentVirtualMediaState = &VirtualMediaState{
		Source:   WebRTC,
		Mode:     mode,
		Filename: filename,
		Size:     size,
	}
	virtualMediaStateMutex.Unlock()

	if err := setMassStorageMode(mode == CDROM); err != nil {
		return fmt.Errorf("failed to set mass storage mode: %w", err)
	}

	logger.Debug().Interface("currentVirtualMediaState", currentVirtualMediaState).Msg("currentVirtualMediaState")
	logger.Debug().Msg("Starting nbd device")
	nbdDevice = NewNBDDevice()
	err := nbdDevice.Start()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to start nbd device")
		return err
	}
	logger.Debug().Msg("nbd device started")
	//TODO: replace by polling on block device having right size
	time.Sleep(1 * time.Second)
	err = setMassStorageImage("/dev/nbd0")
	if err != nil {
		return err
	}
	logger.Info().Msg("usb mass storage mounted")
	return nil
}

func rpcMountWithStorage(filename string, mode VirtualMediaMode) error {
	filename, err := sanitizeFilename(filename)
	if err != nil {
		return err
	}

	virtualMediaStateMutex.Lock()
	defer virtualMediaStateMutex.Unlock()
	if currentVirtualMediaState != nil {
		// Auto-unmount before mounting new image
		logger.Info().Msg("auto-unmounting current image before mounting new one")
		if err := setMassStorageImage("\n"); err != nil {
			logger.Warn().Err(err).Msg("failed to unmount current image")
		}
		time.Sleep(500 * time.Millisecond)
		if nbdDevice != nil {
			nbdDevice.Close()
			nbdDevice = nil
		}
		currentVirtualMediaState = nil
		clearPersistedVirtualMediaState()
	}

	fullPath := filepath.Join(imagesFolder, filename)
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Errorf("[rpcMountWithStorage]failed to get file info: %w", err)
	}

	if err := setMassStorageMode(mode == CDROM); err != nil {
		return fmt.Errorf("failed to set mass storage mode: %w", err)
	}

	err = setMassStorageImage(fullPath)
	if err != nil {
		return fmt.Errorf("failed to set mass storage image: %w", err)
	}
	currentVirtualMediaState = &VirtualMediaState{
		Source:   Storage,
		Mode:     mode,
		Filename: filename,
		Path:     fullPath,
		Size:     fileInfo.Size(),
	}
	persistVirtualMediaState(currentVirtualMediaState)
	return nil
}

func rpcMountWithSDStorage(filename string, mode VirtualMediaMode) error {
	filename, err := sanitizeFilename(filename)
	if err != nil {
		return err
	}

	virtualMediaStateMutex.Lock()
	defer virtualMediaStateMutex.Unlock()
	if currentVirtualMediaState != nil {
		// Auto-unmount before mounting new image
		logger.Info().Msg("auto-unmounting current image before mounting new one")
		if err := setMassStorageImage("\n"); err != nil {
			logger.Warn().Err(err).Msg("failed to unmount current image")
		}
		time.Sleep(500 * time.Millisecond)
		if nbdDevice != nil {
			nbdDevice.Close()
			nbdDevice = nil
		}
		currentVirtualMediaState = nil
		clearPersistedVirtualMediaState()
	}

	fullPath := filepath.Join(SDImagesFolder, filename)
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Errorf("[rpcMountWithSDStorage]failed to get file info: %w", err)
	}

	if err := setMassStorageMode(mode == CDROM); err != nil {
		return fmt.Errorf("failed to set mass storage mode: %w", err)
	}

	err = setMassStorageImage(fullPath)
	if err != nil {
		return fmt.Errorf("failed to set mass storage image: %w", err)
	}
	currentVirtualMediaState = &VirtualMediaState{
		Source:   SDStorage,
		Mode:     mode,
		Filename: filename,
		Path:     fullPath,
		Size:     fileInfo.Size(),
	}
	persistVirtualMediaState(currentVirtualMediaState)
	return nil
}

type StorageSpace struct {
	BytesUsed int64 `json:"bytesUsed"`
	BytesFree int64 `json:"bytesFree"`
}

func rpcGetStorageSpace() (*StorageSpace, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(imagesFolder, &stat)
	if err != nil {
		return nil, fmt.Errorf("failed to get storage stats: %v", err)
	}

	totalSpace := stat.Blocks * uint64(stat.Bsize)
	freeSpace := stat.Bfree * uint64(stat.Bsize)
	usedSpace := totalSpace - freeSpace

	return &StorageSpace{
		BytesUsed: int64(usedSpace),
		BytesFree: int64(freeSpace),
	}, nil
}

type StorageFile struct {
	Filename  string    `json:"filename"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

type StorageFiles struct {
	Files []StorageFile `json:"files"`
}

func rpcListStorageFiles() (*StorageFiles, error) {
	files, err := os.ReadDir(imagesFolder)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %v", err)
	}

	storageFiles := make([]StorageFile, 0)
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		info, err := file.Info()
		if err != nil {
			return nil, fmt.Errorf("failed to get file info: %v", err)
		}

		storageFiles = append(storageFiles, StorageFile{
			Filename:  file.Name(),
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
		})
	}

	return &StorageFiles{Files: storageFiles}, nil
}

func sanitizeFilename(filename string) (string, error) {
	cleanPath := filepath.Clean(filename)
	if filepath.IsAbs(cleanPath) || strings.Contains(cleanPath, "..") {
		return "", errors.New("invalid filename")
	}
	sanitized := filepath.Base(cleanPath)
	if sanitized == "." || sanitized == string(filepath.Separator) {
		return "", errors.New("invalid filename")
	}
	return sanitized, nil
}

func rpcDeleteStorageFile(filename string) error {
	sanitizedFilename, err := sanitizeFilename(filename)
	if err != nil {
		return err
	}

	fullPath := filepath.Join(imagesFolder, sanitizedFilename)

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filename)
	}

	err = os.Remove(fullPath)
	if err != nil {
		return fmt.Errorf("failed to delete file: %v", err)
	}

	// Clear auto-mount config if the deleted file was set for auto-mount
	if config.AutoMountImage != nil && config.AutoMountImage.Filename == sanitizedFilename && config.AutoMountImage.Source == "kvm" {
		config.AutoMountImage = nil
		if err := SaveConfig(); err != nil {
			usbLogger.Warn().Err(err).Msg("failed to clear auto-mount config after file deletion")
		}
	}

	return nil
}

type StorageFileUpload struct {
	AlreadyUploadedBytes int64  `json:"alreadyUploadedBytes"`
	DataChannel          string `json:"dataChannel"`
}

const uploadIdPrefix = "upload_"

func rpcStartStorageFileUpload(filename string, size int64) (*StorageFileUpload, error) {
	sanitizedFilename, err := sanitizeFilename(filename)
	if err != nil {
		return nil, err
	}

	filePath := path.Join(imagesFolder, sanitizedFilename)
	uploadPath := filePath + ".incomplete"

	if _, err := os.Stat(filePath); err == nil {
		return nil, fmt.Errorf("file already exists: %s", sanitizedFilename)
	}

	var alreadyUploadedBytes int64 = 0
	if stat, err := os.Stat(uploadPath); err == nil {
		alreadyUploadedBytes = stat.Size()
	}

	uploadId := uploadIdPrefix + uuid.New().String()
	file, err := os.OpenFile(uploadPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file for upload: %v", err)
	}
	pendingUploadsMutex.Lock()
	pendingUploads[uploadId] = pendingUpload{
		File:                 file,
		Size:                 size,
		AlreadyUploadedBytes: alreadyUploadedBytes,
	}
	pendingUploadsMutex.Unlock()
	return &StorageFileUpload{
		AlreadyUploadedBytes: alreadyUploadedBytes,
		DataChannel:          uploadId,
	}, nil
}

type pendingUpload struct {
	File                 *os.File
	Size                 int64
	AlreadyUploadedBytes int64
}

var pendingUploads = make(map[string]pendingUpload)
var pendingUploadsMutex sync.Mutex

type UploadProgress struct {
	Size                 int64
	AlreadyUploadedBytes int64
}

func handleUploadChannel(d *webrtc.DataChannel) {
	defer d.Close()
	uploadId := d.Label()
	pendingUploadsMutex.Lock()
	pendingUpload, ok := pendingUploads[uploadId]
	pendingUploadsMutex.Unlock()
	if !ok {
		logger.Warn().Str("uploadId", uploadId).Msg("upload channel opened for unknown upload")
		return
	}
	totalBytesWritten := pendingUpload.AlreadyUploadedBytes
	defer func() {
		pendingUpload.File.Close()
		if totalBytesWritten == pendingUpload.Size {
			newName := strings.TrimSuffix(pendingUpload.File.Name(), ".incomplete")
			err := os.Rename(pendingUpload.File.Name(), newName)
			if err != nil {
				logger.Warn().Err(err).Str("uploadId", uploadId).Msg("failed to rename uploaded file")
			} else {
				logger.Debug().Str("uploadId", uploadId).Str("newName", newName).Msg("successfully renamed uploaded file")
			}
		} else {
			logger.Warn().Str("uploadId", uploadId).Msg("uploaded ended before the complete file received")
		}
		pendingUploadsMutex.Lock()
		delete(pendingUploads, uploadId)
		pendingUploadsMutex.Unlock()
	}()
	uploadComplete := make(chan struct{})
	lastProgressTime := time.Now()
	d.OnMessage(func(msg webrtc.DataChannelMessage) {
		bytesWritten, err := pendingUpload.File.Write(msg.Data)
		if err != nil {
			logger.Warn().Err(err).Str("uploadId", uploadId).Msg("failed to write to file")
			close(uploadComplete)
			return
		}
		totalBytesWritten += int64(bytesWritten)

		sendProgress := time.Since(lastProgressTime) >= 200*time.Millisecond
		if totalBytesWritten >= pendingUpload.Size {
			sendProgress = true
			close(uploadComplete)
		}

		if sendProgress {
			progress := UploadProgress{
				Size:                 pendingUpload.Size,
				AlreadyUploadedBytes: totalBytesWritten,
			}
			progressJSON, err := json.Marshal(progress)
			if err != nil {
				logger.Warn().Err(err).Str("uploadId", uploadId).Msg("failed to marshal upload progress")
			} else {
				err = d.SendText(string(progressJSON))
				if err != nil {
					logger.Warn().Err(err).Str("uploadId", uploadId).Msg("failed to send upload progress")
				}
			}
			lastProgressTime = time.Now()
		}
	})

	// Block until upload is complete
	<-uploadComplete
}

func handleUploadHttp(c *gin.Context) {
	uploadId := c.Query("uploadId")
	pendingUploadsMutex.Lock()
	pendingUpload, ok := pendingUploads[uploadId]
	pendingUploadsMutex.Unlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Upload not found"})
		return
	}

	totalBytesWritten := pendingUpload.AlreadyUploadedBytes
	defer func() {
		pendingUpload.File.Close()
		if totalBytesWritten == pendingUpload.Size {
			newName := strings.TrimSuffix(pendingUpload.File.Name(), ".incomplete")
			err := os.Rename(pendingUpload.File.Name(), newName)
			if err != nil {
				logger.Warn().Err(err).Str("uploadId", uploadId).Msg("failed to rename uploaded file")
			} else {
				logger.Debug().Str("uploadId", uploadId).Str("newName", newName).Msg("successfully renamed uploaded file")
			}
		} else {
			logger.Warn().Str("uploadId", uploadId).Msg("uploaded ended before the complete file received")
		}
		pendingUploadsMutex.Lock()
		delete(pendingUploads, uploadId)
		pendingUploadsMutex.Unlock()
	}()

	reader := c.Request.Body
	buffer := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buffer)
		if err != nil && err != io.EOF {
			logger.Warn().Err(err).Str("uploadId", uploadId).Msg("failed to read from request body")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read upload data"})
			return
		}

		if n > 0 {
			bytesWritten, err := pendingUpload.File.Write(buffer[:n])
			if err != nil {
				logger.Warn().Err(err).Str("uploadId", uploadId).Msg("failed to write to file")
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write upload data"})
				return
			}
			totalBytesWritten += int64(bytesWritten)
		}

		if err == io.EOF {
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Upload completed"})
}

func handleDownloadHttp(c *gin.Context) {
	filename := c.Query("file")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file parameter"})
		return
	}

	sanitizedFilename, err := sanitizeFilename(filename)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid filename"})
		return
	}

	fullPath := filepath.Join(imagesFolder, sanitizedFilename)

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", sanitizedFilename))
	c.FileAttachment(fullPath, sanitizedFilename)
}

// SD Card
func rpcGetSDStorageSpace() (*StorageSpace, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(SDImagesFolder, &stat)
	if err != nil {
		return nil, fmt.Errorf("failed to get SD storage stats: %v", err)
	}

	totalSpace := stat.Blocks * uint64(stat.Bsize)
	freeSpace := stat.Bfree * uint64(stat.Bsize)
	usedSpace := totalSpace - freeSpace

	return &StorageSpace{
		BytesUsed: int64(usedSpace),
		BytesFree: int64(freeSpace),
	}, nil
}

func rpcResetSDStorage() error {
	err := os.WriteFile("/sys/bus/platform/drivers/dwmmc_rockchip/unbind", []byte("ffaa0000.mmc"), 0644)
	if err != nil {
		logger.Error().Err(err).Msg("failed to unbind mmc")
	}
	time.Sleep(100 * time.Millisecond)
	err = os.WriteFile("/sys/bus/platform/drivers/dwmmc_rockchip/bind", []byte("ffaa0000.mmc"), 0644)
	if err != nil {
		logger.Error().Err(err).Msg("failed to bind mmc")
	}
	time.Sleep(500 * time.Millisecond)

	err = updateMtpWithSDStatus()
	if err != nil {
		return err
	}

	return nil
}

func rpcMountSDStorage() error {
	err := os.WriteFile("/sys/bus/platform/drivers/dwmmc_rockchip/bind", []byte("ffaa0000.mmc"), 0644)
	if err != nil {
		logger.Error().Err(err).Msg("failed to bind mmc")
	}
	time.Sleep(500 * time.Millisecond)

	err = updateMtpWithSDStatus()
	if err != nil {
		return err
	}

	return nil
}

func rpcUnmountSDStorage() error {
	err := os.WriteFile("/sys/bus/platform/drivers/dwmmc_rockchip/unbind", []byte("ffaa0000.mmc"), 0644)
	if err != nil {
		logger.Error().Err(err).Msg("failed to unbind mmc")
	}
	time.Sleep(500 * time.Millisecond)

	err = updateMtpWithSDStatus()
	if err != nil {
		return err
	}

	return nil
}

func rpcFormatSDStorage(confirm bool, fsType string) error {
	validFsTypes := map[string]bool{"exfat": true, "fat32": true}
	if !validFsTypes[fsType] {
		fsType = "fat32"
	}
	if !confirm {
		return fmt.Errorf("format not confirmed")
	}
	if _, err := os.Stat("/dev/mmcblk1"); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("sd device not found: /dev/mmcblk1")
		}
		return fmt.Errorf("failed to stat sd device: %w", err)
	}

	if err := updateMtp(false); err != nil {
		logger.Error().Err(err).Msg("failed to update mtp before formatting sd")
	}

	if out, err := exec.Command("mount").Output(); err == nil {
		if strings.Contains(string(out), " on /mnt/sdcard") {
			if umOut, umErr := exec.Command("umount", "/mnt/sdcard").CombinedOutput(); umErr != nil {
				return fmt.Errorf("failed to unmount sdcard: %w: %s", umErr, strings.TrimSpace(string(umOut)))
			}
		}
	}

	if err := os.MkdirAll(SDImagesFolder, 0755); err != nil {
		return fmt.Errorf("failed to ensure mount point: %w", err)
	}

	if _, err := os.Stat("/dev/mmcblk1p1"); os.IsNotExist(err) {
		var lastErr error
		if _, err := exec.LookPath("sfdisk"); err == nil {
			sfdiskInput := "label: dos\nunit: sectors\n\n2048,,c,*\n"
			cmd := exec.Command("sfdisk", "/dev/mmcblk1")
			cmd.Stdin = bytes.NewBufferString(sfdiskInput)
			partOut, partErr := cmd.CombinedOutput()
			if partErr != nil {
				lastErr = fmt.Errorf("sfdisk failed: %w: %s", partErr, strings.TrimSpace(string(partOut)))
			} else {
				lastErr = nil
			}
		} else if _, err := exec.LookPath("fdisk"); err == nil {
			fdiskScript := "o\nn\np\n1\n2048\n\nt\n1\nc\na\n1\nw\n"
			cmd := exec.Command("fdisk", "/dev/mmcblk1")
			cmd.Stdin = bytes.NewBufferString(fdiskScript)
			partOut, partErr := cmd.CombinedOutput()
			if partErr != nil {
				lastErr = fmt.Errorf("fdisk failed: %w: %s", partErr, strings.TrimSpace(string(partOut)))
			} else {
				lastErr = nil
			}
		} else if _, err := exec.LookPath("parted"); err == nil {
			partedOut, partedErr := exec.Command("parted", "-s", "/dev/mmcblk1", "mklabel", "msdos", "mkpart", "primary", "fat32", "1MiB", "100%").CombinedOutput()
			if partedErr != nil {
				lastErr = fmt.Errorf("parted failed: %w: %s", partedErr, strings.TrimSpace(string(partedOut)))
			} else {
				lastErr = nil
			}
		} else {
			return fmt.Errorf("no partitioning tool found (need sfdisk, fdisk, or parted)")
		}

		if lastErr != nil {
			return fmt.Errorf("failed to create sd partition: %w", lastErr)
		}

		if _, err := exec.LookPath("partprobe"); err == nil {
			if _, err := exec.Command("partprobe", "/dev/mmcblk1").CombinedOutput(); err != nil {
				time.Sleep(800 * time.Millisecond)
			} else {
				time.Sleep(300 * time.Millisecond)
			}
		} else {
			time.Sleep(800 * time.Millisecond)
		}
	}

	if _, err := os.Stat("/dev/mmcblk1p1"); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("sd partition not found after partitioning: /dev/mmcblk1p1")
		}
		return fmt.Errorf("failed to stat sd partition: %w", err)
	}

	var mkfsCmd *exec.Cmd
	if fsType == "exfat" {
		mkfsCmd = exec.Command("mkfs.exfat", "-n", "PICOKVM", "/dev/mmcblk1p1")
	} else {
		mkfsCmd = exec.Command("mkfs.vfat", "-F", "32", "-n", "PICOKVM", "/dev/mmcblk1p1")
	}
	mkfsOut, mkfsErr := mkfsCmd.CombinedOutput()
	if mkfsErr != nil {
		return fmt.Errorf("failed to format sdcard: %w: %s", mkfsErr, strings.TrimSpace(string(mkfsOut)))
	}

	mountOut, mountErr := exec.Command("mount", "/dev/mmcblk1p1", SDImagesFolder).CombinedOutput()
	if mountErr != nil {
		return fmt.Errorf("failed to mount sdcard after format: %w: %s", mountErr, strings.TrimSpace(string(mountOut)))
	}

	SyncConfigSD(false)

	if err := updateMtp(true); err != nil {
		return fmt.Errorf("failed to update mtp after formatting sd: %w", err)
	}

	return nil
}

func rpcListSDStorageFiles() (*StorageFiles, error) {
	files, err := os.ReadDir(SDImagesFolder)
	if err != nil {
		time.Sleep(500 * time.Millisecond)
		files, err = os.ReadDir(SDImagesFolder)
		if err != nil {
			return nil, fmt.Errorf("failed to read directory: %v", err)
		}
	}

	storageFiles := make([]StorageFile, 0)
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		info, err := file.Info()
		if err != nil {
			return nil, fmt.Errorf("[rpcListSDStorageFiles]failed to get file info: %v", err)
		}

		storageFiles = append(storageFiles, StorageFile{
			Filename:  file.Name(),
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
		})
	}

	return &StorageFiles{Files: storageFiles}, nil
}

func rpcDeleteSDStorageFile(filename string) error {
	sanitizedFilename, err := sanitizeFilename(filename)
	if err != nil {
		return err
	}

	fullPath := filepath.Join(SDImagesFolder, sanitizedFilename)

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filename)
	}

	err = os.Remove(fullPath)
	if err != nil {
		return fmt.Errorf("failed to delete file: %v", err)
	}

	// Clear auto-mount config if the deleted file was set for auto-mount
	if config.AutoMountImage != nil && config.AutoMountImage.Filename == sanitizedFilename && config.AutoMountImage.Source == "sd" {
		config.AutoMountImage = nil
		if err := SaveConfig(); err != nil {
			usbLogger.Warn().Err(err).Msg("failed to clear auto-mount config after file deletion")
		}
	}

	return nil
}

type SDMountStatus string

const (
	SDMountOK   SDMountStatus = "ok"
	SDMountNone SDMountStatus = "none"
	SDMountFail SDMountStatus = "fail"
)

type SDMountStatusResponse struct {
	Status SDMountStatus `json:"status"`
	Reason string        `json:"reason,omitempty"`
}

func rpcGetSDMountStatus() (*SDMountStatusResponse, error) {
	// If system is booting from SD card, SD storage is not available as separate mount
	if IsBootFromSD() {
		return &SDMountStatusResponse{Status: SDMountFail, Reason: "boot_device"}, nil
	}

	if _, err := os.Stat("/dev/mmcblk1"); os.IsNotExist(err) {
		return &SDMountStatusResponse{Status: SDMountNone}, nil
	}

	if _, err := os.Stat("/dev/mmcblk1p1"); os.IsNotExist(err) {
		return &SDMountStatusResponse{Status: SDMountFail, Reason: "no_partition"}, nil
	}

	output, err := exec.Command("mount").Output()
	if err != nil {
		return &SDMountStatusResponse{Status: SDMountFail, Reason: "check_mount_failed"}, fmt.Errorf("failed to check mount status: %v", err)
	}

	mountOutput := string(output)
	if strings.Contains(mountOutput, "/dev/mmcblk1p1 on /mnt/sdcard") {
		return &SDMountStatusResponse{Status: SDMountOK}, nil
	}

	err = exec.Command("mount", "/dev/mmcblk1p1", "/mnt/sdcard").Run()
	if err != nil {
		return &SDMountStatusResponse{Status: SDMountFail, Reason: "mount_failed"}, fmt.Errorf("failed to mount SD card: %v", err)
	}

	output, err = exec.Command("mount").Output()
	if err != nil {
		return &SDMountStatusResponse{Status: SDMountFail, Reason: "check_mount_after_failed"}, fmt.Errorf("failed to check mount status after mounting: %v", err)
	}

	if strings.Contains(string(output), "/dev/mmcblk1p1 on /mnt/sdcard") {
		return &SDMountStatusResponse{Status: SDMountOK}, nil
	}

	return &SDMountStatusResponse{Status: SDMountFail, Reason: "mount_unknown"}, nil
}

type SDStorageFileUpload struct {
	AlreadyUploadedBytes int64  `json:"alreadyUploadedBytes"`
	DataChannel          string `json:"dataChannel"`
}

const SDUploadIdPrefix = "upload_"

func rpcStartSDStorageFileUpload(filename string, size int64) (*SDStorageFileUpload, error) {
	sanitizedFilename, err := sanitizeFilename(filename)
	if err != nil {
		return nil, err
	}

	filePath := path.Join(SDImagesFolder, sanitizedFilename)
	uploadPath := filePath + ".incomplete"

	if _, err := os.Stat(filePath); err == nil {
		return nil, fmt.Errorf("file already exists: %s", sanitizedFilename)
	}

	var alreadyUploadedBytes int64 = 0
	if stat, err := os.Stat(uploadPath); err == nil {
		alreadyUploadedBytes = stat.Size()
	}

	uploadId := SDUploadIdPrefix + uuid.New().String()
	file, err := os.OpenFile(uploadPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file for upload: %v", err)
	}
	pendingUploadsMutex.Lock()
	pendingUploads[uploadId] = pendingUpload{
		File:                 file,
		Size:                 size,
		AlreadyUploadedBytes: alreadyUploadedBytes,
	}
	pendingUploadsMutex.Unlock()
	return &SDStorageFileUpload{
		AlreadyUploadedBytes: alreadyUploadedBytes,
		DataChannel:          uploadId,
	}, nil
}

func handleSDDownloadHttp(c *gin.Context) {
	filename := c.Query("file")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file parameter"})
		return
	}

	sanitizedFilename, err := sanitizeFilename(filename)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid filename"})
		return
	}

	fullPath := filepath.Join(SDImagesFolder, sanitizedFilename)

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", sanitizedFilename))
	c.FileAttachment(fullPath, sanitizedFilename)
}

const umtprdConfPath = "/etc/umtprd/umtprd.conf"

var umtprdWriteLock sync.Mutex

func writeUmtprdConfFile(withSD bool) error {
	umtprdWriteLock.Lock()
	defer umtprdWriteLock.Unlock()

	if err := os.MkdirAll(filepath.Dir(umtprdConfPath), 0755); err != nil {
		return fmt.Errorf("failed to create umtprd.conf dir: %w", err)
	}

	conf := `loop_on_disconnect 1

storage "/userdata/picokvm/share" "share folder" "rw"
`
	if withSD {
		conf += `storage "/mnt/sdcard" "sdcard folder" "rw"
`
	}

	conf += fmt.Sprintf(`
manufacturer "%s"
product "%s"
serial "%s"

usb_vendor_id   %s
usb_product_id  %s

usb_functionfs_mode 0x1

usb_dev_path   "/dev/ffs-mtp/ep0"
usb_epin_path  "/dev/ffs-mtp/ep1"
usb_epout_path "/dev/ffs-mtp/ep2"
usb_epint_path "/dev/ffs-mtp/ep3"
usb_max_packet_size 0x200
`,
		config.UsbConfig.Manufacturer,
		config.UsbConfig.Product,
		config.UsbConfig.SerialNumber,
		config.UsbConfig.VendorId,
		config.UsbConfig.ProductId,
	)

	return os.WriteFile(umtprdConfPath, []byte(conf), 0644)
}

func updateMtp(withSD bool) error {
	if err := writeUmtprdConfFile(withSD); err != nil {
		logger.Error().Err(err).Msg("failed to write umtprd conf file")
	}
	if config.UsbDevices.Mtp {
		if err := gadget.UnbindUDC(); err != nil {
			logger.Error().Err(err).Msg("failed to unbind gadget from UDC")
		}

		if out, err := exec.Command("pgrep", "-x", "umtprd").Output(); err == nil && len(out) > 0 {
			cmd := exec.Command("killall", "umtprd")
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to killall umtprd: %w", err)
			}
		}

		cmd := exec.Command("umtprd")
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to exec binary: %w", err)
		}

		var lastErr error
		for attempt := 0; attempt < 6; attempt++ {
			if err := rpcSetUsbDevices(*config.UsbDevices); err == nil {
				lastErr = nil
				break
			} else {
				lastErr = err
				logger.Warn().
					Int("attempt", attempt+1).
					Err(err).
					Msg("failed to re-apply usb devices after mtp update, retrying")
				time.Sleep(time.Duration(300*(attempt+1)) * time.Millisecond)
			}
		}
		if lastErr != nil {
			return fmt.Errorf("failed to set usb devices after mtp update: %w", lastErr)
		}
	}

	return nil
}

func updateMtpWithSDStatus() error {
	resp, _ := rpcGetSDMountStatus()
	return updateMtp(resp.Status == SDMountOK)
}
