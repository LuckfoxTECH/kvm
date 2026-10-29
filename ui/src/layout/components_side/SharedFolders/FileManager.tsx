// StorageFilePage.tsx
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { LuRefreshCw } from "react-icons/lu";
import { PlusCircleIcon } from "@heroicons/react/20/solid";
import { useNavigate } from "react-router-dom";
import { useReactAt } from 'i18n-auto-extractor/react'
import { Typography , Button as AntdButton } from "antd";
import OnSDCardSvg from "@assets/second/noSD.svg?react"
import RefreshSvg from "@assets/second/refresh.svg?react"

import Card from "@components/Card";
import { formatters } from "@/utils";
import { DEVICE_API } from "@/ui.config";
import { useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import { FileUploader } from "@components/FileManager/FileUploader";
import { PreUploadedImageItem } from "@components/PreUploadedImageItem";
import { dark_bd_style, dark_bg_desktop, dark_font_style , text_primary_color } from "@/layout/theme_color";

const { Title } = Typography;

export interface StorageFile {
  name: string;
  size: string;
  createdAt: string;
}

export interface StorageSpace {
  bytesUsed: number;
  bytesFree: number;
}

const LoadingOverlay: React.FC = () => {
  const { $at } = useReactAt();

  return (
    <div className="absolute inset-0 bg-white/50 dark:bg-slate-800/50 flex items-center justify-center z-10 rounded-lg">
      <div className="bg-white dark:bg-slate-800 rounded-lg p-6 shadow-lg border border-slate-200 dark:border-slate-700">
        <div className="flex items-center gap-3">
          <LuRefreshCw className="h-5 w-5 animate-spin text-[rgba(22,152,217,1)] dark:text-[rgba(45,106,229,1)]" />
          <span className="text-sm font-medium">{$at("Processing...")}</span>
        </div>
      </div>
    </div>
  );
};

export interface StorageFiles {
  files: {
    filename: string;
    size: number;
    createdAt: string;
  }[];
}

interface StorageFilePageProps {
  /** Set when the manager is embedded in a drawer that already has a title. */
  hideTitle?: boolean;
  mediaType: "local" | "sd";
  returnTo: string;

  listFilesMethod: string;
  getSpaceMethod: string;
  deleteFileMethod: string;
  downloadUrlPrefix: string;

  showSDManagement?: boolean;
  onResetSDStorage?: () => void;
  onUnmountSDStorage?: () => void;
  onFormatSDStorage?: () => void;
  onMountSDStorage?: () => void;
  fsType?: 'exfat' | 'fat32';
  onFsTypeChange?: (value: 'exfat' | 'fat32') => void;
}

export const FileManager: React.FC<StorageFilePageProps> = ({
                                                              mediaType,
                                                              listFilesMethod,
                                                              getSpaceMethod,
                                                              deleteFileMethod,
                                                              downloadUrlPrefix,
                                                              showSDManagement = false,
                                                              onResetSDStorage,
                                                              onUnmountSDStorage,
                                                              onFormatSDStorage,
                                                              fsType,
                                                              onFsTypeChange,
                                                              hideTitle = false,
                                                            }) => {
  const { $at } = useReactAt();

  const [onStorageFiles, setOnStorageFiles] = useState<StorageFile[]>([]);
  const [sdMountStatus, setSDMountStatus] = useState<"ok" | "none" | "fail" | null>(
    mediaType === "sd" ? null : "ok"
  );
  const [currentPage, setCurrentPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const filesPerPage = 5;

  const [send] = useJsonRpc();
  const [storageSpace, setStorageSpace] = useState<StorageSpace | null>(null);

  const [uploadFile, setUploadFile] = useState<string | null>(null);

  const percentageUsed = useMemo(() => {
    if (!storageSpace) return 0;
    return Number(
      (
        (storageSpace.bytesUsed / (storageSpace.bytesUsed + storageSpace.bytesFree)) *
        100
      ).toFixed(1),
    );
  }, [storageSpace]);

  const bytesUsed = useMemo(() => storageSpace?.bytesUsed || 0, [storageSpace]);
  const bytesFree = useMemo(() => storageSpace?.bytesFree || 0, [storageSpace]);

  const syncStorage = useCallback(() => {
    if (mediaType === "sd") {
      send("getSDMountStatus", {}, res => {
        if ("error" in res) {
          notifications.error(`Failed to check SD card status: ${res.error}`);
          setSDMountStatus(null);
          return;
        }

        const { status } = res.result as { status: "ok" | "none" | "fail" };
        setSDMountStatus(status);

        if (status !== "ok") return;

        fetchFilesAndSpace();
      });
    } else {
      fetchFilesAndSpace();
    }
  }, [send, mediaType]);

  const fetchFilesAndSpace = useCallback(() => {
    send(listFilesMethod, {}, res => {
      if ("error" in res) {
        notifications.error(`Error listing storage files: ${res.error}`);
        return;
      }
      const { files } = res.result as StorageFiles;
      const formattedFiles = files.map(file => ({
        name: file.filename,
        size: formatters.bytes(file.size),
        createdAt: formatters.date(new Date(file?.createdAt)),
      }));
      setOnStorageFiles(formattedFiles);
    });

    send(getSpaceMethod, {}, res => {
      if ("error" in res) {
        notifications.error(`Error getting storage space: ${res.error}`);
        return;
      }
      const space = res.result as StorageSpace;
      setStorageSpace(space);
    });
  }, [send, listFilesMethod, getSpaceMethod]);

  useEffect(() => {
    syncStorage();
  }, [syncStorage]);

  const handleDeleteFile = useCallback((file: StorageFile) => {
    send(deleteFileMethod, { filename: file.name }, res => {
      if ("error" in res) {
        notifications.error(`Error deleting file: ${res.error}`);
        return;
      }
      syncStorage();
    });
  }, [send, deleteFileMethod, syncStorage]);

  const handleDownloadFile = useCallback((file: StorageFile) => {
    const downloadUrl = `${DEVICE_API}${downloadUrlPrefix}?file=${encodeURIComponent(file.name)}`;
    const a = document.createElement("a");
    a.href = downloadUrl;
    a.download = file.name;
    document.body.appendChild(a);
    a.click();
    a.remove();
  }, [downloadUrlPrefix]);

  const handleNewImageClick = useCallback((incompleteFileName?: string) => {
    if (incompleteFileName) {
      setUploadFile(incompleteFileName);
    } else {
      setUploadFile(null);
    }
  }, []);

  const indexOfLastFile = currentPage * filesPerPage;
  const indexOfFirstFile = indexOfLastFile - filesPerPage;
  const currentFiles = onStorageFiles.slice(indexOfFirstFile, indexOfLastFile);
  const totalPages = Math.ceil(onStorageFiles.length / filesPerPage);

  const handlePreviousPage = () => setCurrentPage(prev => Math.max(prev - 1, 1));
  const handleNextPage = () => setCurrentPage(prev => Math.min(prev + 1, totalPages));

  const handleUnmountWrapper = useCallback(async () => {
    if (onUnmountSDStorage) {
      setLoading(true);
      await onUnmountSDStorage();
      setSDMountStatus(null);
      syncStorage();
      setLoading(false);
    }
  }, [onUnmountSDStorage, syncStorage]);

  const handleResetWrapper = useCallback(async () => {
    if (onResetSDStorage) {
      setLoading(true);
      await onResetSDStorage();
      setSDMountStatus(null);
      syncStorage();
      setLoading(false);
    }
  }, [onResetSDStorage, syncStorage]);

  const handleFormatWrapper = useCallback(async () => {
    if (onFormatSDStorage) {
      setLoading(true);
      await onFormatSDStorage();
      setSDMountStatus(null);
      syncStorage();
      setLoading(false);
    }
  }, [onFormatSDStorage, syncStorage]);

  if (mediaType === "sd" && sdMountStatus && sdMountStatus !== "ok") {
    return (
      <div className="mx-auto max-w-4xl py-8">
        <div className="w-full space-y-6 px-0.5">
          <Card>
            <div className="p-8 text-center relative">
              <div className="space-y-2">
                <OnSDCardSvg className="mx-auto h-[24px] w-[24px]" />
                <div className="space-y-2">
                  <div className={"flex justify-center gap-3 pt-4"}>
                    <h3 className="text-lg font-semibold text-black dark:text-white">
                      {sdMountStatus === "none"
                        ? $at("No SD Card Detected")
                        : $at("SD Card Mount Failed")}
                    </h3>
                    <div className={`w-[24px] h-[24px] border ${dark_bd_style} p-[5px] ${dark_bg_desktop} flex items-center justify-center`}
                         onClick={handleResetWrapper}>
                        <RefreshSvg className={`h-[12px] w-[12px] ${dark_font_style}`}  />
                    </div>
                  </div>
                  <p className="text-slate-700 dark:text-slate-300">
                    {sdMountStatus === "none"
                      ? $at("Please insert an SD card and try again.")
                      : $at("Please format the SD card and try again.")}
                  </p>
                  {sdMountStatus !== "none" && (
                    <div className="pt-2">
                      <div className="w-full space-y-2">
                        <p className="w-full text-left text-xs text-slate-700 dark:text-slate-300">
                          {$at("Choose the file system for MicroSD formatting")}
                        </p>
                        <select
                          value={fsType || "fat32"}
                          onChange={(e) => onFsTypeChange?.(e.target.value as 'exfat' | 'fat32')}
                          style={{ width: "100%", padding: "8px", borderRadius: "4px" }}
                        >
                          <option value="fat32">FAT32</option>
                          <option value="exfat">exFAT</option>
                        </select>
                        <AntdButton
                          disabled={loading}
                          danger={true}
                          type="primary"
                          onClick={handleFormatWrapper}
                          className="w-full text-red-500 dark:text-red-400 border-red-200 dark:border-red-800"
                        >
                          {$at("Format MicroSD Card")} ({(fsType || "fat32")})
                        </AntdButton>
                      </div>
                    </div>
                  )}
                </div>

              </div>
              {loading && <LoadingOverlay />}
            </div>
          </Card>
        </div>
      </div>
    );
  }

  return (
    <div className="w-full space-y-6 px-0.5">
      {!hideTitle && (
        <Title level={5} style={{ marginBottom: "24px" }}>
          { (mediaType === "sd")
            ? $at("Manage Shared Folders in KVM MicroSD Card")
            : $at("Manage Shared Folders in KVM Storage")
          }
        </Title>
      )}

      <FileListSection
        files={onStorageFiles}
        currentFiles={currentFiles}
        loading={loading}
        onDelete={handleDeleteFile}
        onDownload={handleDownloadFile}
        onNewImageClick={handleNewImageClick}
        showPagination={onStorageFiles.length > filesPerPage}
        paginationInfo={{
          indexOfFirstFile,
          indexOfLastFile,
          totalFiles: onStorageFiles.length,
          currentPage,
          totalPages
        }}
        onPreviousPage={handlePreviousPage}
        onNextPage={handleNextPage}
      />

      <hr className="border-slate-800/20 dark:border-slate-300/20" />
      <div className="animate-fadeIn space-y-2 opacity-0" style={{ animationDuration: "0.7s", animationDelay: "0.20s" }}>
        <StorageSpaceBar
          percentageUsed={percentageUsed}
          bytesUsed={bytesUsed}
          bytesFree={bytesFree}
        />
      </div>

      <ActionButtonsSection
        mediaType={mediaType}
        loading={loading}
        showSDManagement={showSDManagement}
        onNewImageClick={handleNewImageClick}
        onUnmountSDStorage={handleUnmountWrapper}
        onFormatSDStorage={handleFormatWrapper}
        syncStorage={syncStorage}
        fsType={fsType}
        onFsTypeChange={onFsTypeChange}
      />

      {uploadFile ? (
        <FileUploader
          key={`resume-${uploadFile}`}
          onBack={() => {
            setUploadFile(null);
            syncStorage();
          }}
          incompleteFileName={uploadFile}
          media={mediaType}
        />
      ) : (
        <FileUploader
          key="new-upload"
          onBack={syncStorage}
          media={mediaType}
        />
      )}

    </div>

  );
};

interface FileListSectionProps {
  files: StorageFile[];
  currentFiles: StorageFile[];
  loading: boolean;
  onDelete: (file: StorageFile) => void;
  onDownload: (file: StorageFile) => void;
  onNewImageClick: (incompleteFileName?: string) => void;
  showPagination: boolean;
  paginationInfo: {
    indexOfFirstFile: number;
    indexOfLastFile: number;
    totalFiles: number;
    currentPage: number;
    totalPages: number;
  };
  onPreviousPage: () => void;
  onNextPage: () => void;
}

const FileListSection: React.FC<FileListSectionProps> = ({
                                                           files,
                                                           currentFiles,
                                                           loading,
                                                           onDelete,
                                                           onDownload,
                                                           onNewImageClick,
                                                           showPagination,
                                                           paginationInfo,
                                                           onPreviousPage,
                                                           onNextPage
                                                         }) => {
  const { $at } = useReactAt();

  if (files.length === 0) {
    return (
      <div className="w-full animate-fadeIn opacity-0"
         style={{ animationDuration: "0.7s", animationDelay: "0.1s" }}
    >
      <div className="relative">
        <Card>
          <div className="flex items-center justify-center py-8 text-center">
            <div className="space-y-3">
              <div className="space-y-1">
                <PlusCircleIcon className={`mx-auto h-6 w-6 ${text_primary_color}`} />
                <h3 className="text-sm leading-none font-semibold text-black dark:text-white">
                  {$at("No files found")}
                </h3>
                <p className="text-xs leading-none text-slate-700 dark:text-slate-300">
                  {$at("Get started by uploading your first file")}
                </p>
              </div>
            </div>
          </div>
        </Card>
        {loading && <LoadingOverlay />}
      </div>
    </div>
  );
  }

  return (
    <div className="w-full animate-fadeIn opacity-0 px-0.5"
         style={{ animationDuration: "0.7s", animationDelay: "0.1s" }}
    >
      <div className="relative">
        <Card>
          <div className="w-full divide-y divide-slate-200 dark:divide-slate-700">
            {currentFiles.map((file, index) => (
              <PreUploadedImageItem
                key={index}
                name={file.name}
                size={file.size}
                uploadedAt={file.createdAt}
                isIncomplete={file.name.endsWith(".incomplete")}
                isSelected={false}
                onDownload={() => {
                  if (window.confirm($at("Are you sure you want to download ") + file.name + "?")) {
                    onDownload(file);
                  }
                }}
                onDelete={() => {
                  if (window.confirm($at("Are you sure you want to delete ") + file.name + "?")) {
                    onDelete(file);
                  }
                }}
                onContinueUpload={() => onNewImageClick(file.name)}
              />
            ))}

            {showPagination && (
              <div className="flex items-center justify-between px-4 py-3 bg-slate-50 dark:bg-slate-800">
                <p className="text-sm text-slate-700 dark:text-slate-300">
                  {$at("Showing")} <span className="font-bold">{paginationInfo.indexOfFirstFile + 1}</span> {$at("to")}{" "}
                  <span className="font-bold">
                    {Math.min(paginationInfo.indexOfLastFile, paginationInfo.totalFiles)}
                  </span>{" "}
                  {$at("of")} <span className="font-bold">{paginationInfo.totalFiles}</span> {$at("results")}
                </p>
                <div className="flex items-center gap-x-2">
                  <AntdButton
                    type="primary"
                    onClick={onPreviousPage}
                    disabled={paginationInfo.currentPage === 1}
                  >{$at("Previous")}</AntdButton>
                  <AntdButton
                    type="primary"
                    onClick={onNextPage}
                    disabled={paginationInfo.currentPage === paginationInfo.totalPages}
                  >{$at("Next")}</AntdButton>
                </div>
              </div>
            )}
          </div>
        </Card>
        {loading && <LoadingOverlay />}
      </div>
    </div>
  );
};

interface StorageSpaceBarProps {
  percentageUsed: number;
  bytesUsed: number;
  bytesFree: number;
}

export default function StorageSpaceBar({ percentageUsed, bytesUsed, bytesFree }: StorageSpaceBarProps) {
  const { $at } = useReactAt();

  return (
    <>
      <div className="flex justify-between text-sm">
        <span className="font-medium text-black dark:text-white">{$at("Available space")}</span>
        <span className="text-slate-700 dark:text-slate-300">{percentageUsed}% {$at("used")}</span>
      </div>
      <div className="h-3.5 w-full overflow-hidden rounded-xs bg-slate-200 dark:bg-slate-700">
        <div
          className="h-full rounded-xs bg-[rgba(22,152,217,1)] transition-all duration-300 ease-in-out dark:bg-[rgba(45,106,229,1)]"
          style={{ width: `${percentageUsed}%` }}
        />
      </div>
      <div className="flex justify-between text-sm text-slate-600">
        <span className="text-slate-700 dark:text-slate-300">
          {formatters.bytes(bytesUsed)} {$at("used")}
        </span>
        <span className="text-slate-700 dark:text-slate-300">
          {formatters.bytes(bytesFree)} {$at("free")}
        </span>
      </div>
    </>
  );
}

interface ActionButtonsSectionProps {
  mediaType: "local" | "sd";
  loading: boolean;
  showSDManagement?: boolean;
  onNewImageClick: (incompleteFileName?: string) => void;
  onUnmountSDStorage?: () => void;
  onFormatSDStorage?: () => void;
  syncStorage: () => void;
  fsType?: 'exfat' | 'fat32';
  onFsTypeChange?: (value: 'exfat' | 'fat32') => void;
}

const ActionButtonsSection: React.FC<ActionButtonsSectionProps> = ({
                                                                      mediaType,
                                                                      loading,
                                                                      showSDManagement,
                                                                      onUnmountSDStorage,
                                                                      onFormatSDStorage,
                                                                      fsType,
                                                                      onFsTypeChange,
                                                                    }) => {
  const { $at } = useReactAt();

  if (mediaType === "sd" && showSDManagement) {
    return (
      <div className="animate-fadeIn space-y-2 opacity-0"
           style={{ animationDuration: "0.7s", animationDelay: "0.25s" }}
      >
        <div className="w-full space-y-2">
          <p className="w-full text-left text-xs text-slate-700 dark:text-slate-300">
            {$at("Choose the file system for MicroSD formatting")}
          </p>
          <select
            value={fsType || "fat32"}
            onChange={(e) => onFsTypeChange?.(e.target.value as 'exfat' | 'fat32')}
            style={{ width: "100%", padding: "8px", borderRadius: "4px" }}
          >
            <option value="fat32">FAT32</option>
            <option value="exfat">exFAT</option>
          </select>
          <AntdButton
            disabled={loading}
            type="primary"
            danger={true}
            onClick={onFormatSDStorage}
            className="w-full text-red-500 dark:text-red-400 border-red-200 dark:border-red-800"
          >{$at("Format MicroSD Card")} ({(fsType || "fat32")})</AntdButton>
        </div>
        <AntdButton
          disabled={loading}
          type="primary"
          danger={true}
          onClick={onUnmountSDStorage}
          className="w-full text-red-500 dark:text-red-400 border-red-200 dark:border-red-800"
        >{$at("Unmount MicroSD Card")}</AntdButton>
      </div>
    );
  }
};
