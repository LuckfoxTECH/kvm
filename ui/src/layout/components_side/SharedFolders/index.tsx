import React from "react";

import SideTabs from "@components/Sidebar/SideTabs";
import DeviceFilePage from "@/layout/components_side/SharedFolders/DeviceFilePage";
import SDFilePage from "@/layout/components_side/SharedFolders/SDFilePage";
import { useBootStorageType } from "@/hooks/useBootStorage";


const SharedFolders: React.FC<{ hideTitle?: boolean }> = ({ hideTitle }) => {
  const { bootStorageType } = useBootStorageType();
  const isBootFromSD = bootStorageType === "sd";

  if (isBootFromSD) {
    return <DeviceFilePage hideTitle={hideTitle} />;
  }

  return (
    <SideTabs
      tab1Label="KVM Storage"
      tab2Label="MicroSD Card"
      tab1Content={<DeviceFilePage hideTitle={hideTitle} />}
      tab2Content={<SDFilePage hideTitle={hideTitle} />}
      defaultActiveKey="1"
    />
  );
};

export default SharedFolders;
