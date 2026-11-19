interface ImgmeshFile {
  name: string;
  data: Uint8Array;
}

interface ImgmeshBridge {
  apiBase: string;
  os: string;
  saveFiles: (files: ImgmeshFile[]) => Promise<string>;
  copy: (text: string) => Promise<void>;
}

interface Window {
  imgmesh?: ImgmeshBridge;
}
