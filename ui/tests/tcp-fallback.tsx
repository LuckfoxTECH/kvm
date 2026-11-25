import { useState } from "react";
import { createRoot } from "react-dom/client";
import { ConnectionFailedOverlay } from "../src/components/VideoOverlay";
import { useSettingsStore } from "../src/hooks/stores";
import "../src/index.css";

// Run with Vite --mode device and open /tests/tcp-fallback.html.
// The fixture uses the real overlay and store, but never contacts a KVM.
function Fixture() {
  const forceHttp = useSettingsStore(state => state.forceHttp);
  const [retries, setRetries] = useState(0);
  return (
    <>
      <p>
        {forceHttp ? "Mode: TCP" : "Mode: WebRTC"} | Retries: {retries}
      </p>
      <main style={{ position: "relative", height: "85vh" }}>
        <ConnectionFailedOverlay
          show
          setupPeerConnection={async () => setRetries(count => count + 1)}
        />
      </main>
    </>
  );
}

createRoot(document.getElementById("root")!).render(<Fixture />);
