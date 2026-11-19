declare module "heic2any" {
  export default function heic2any(options: { blob: Blob; toType?: string; quality?: number }): Promise<Blob | Blob[]>;
}

const previewLimit = 25 * 1024 * 1024;

export async function heicJPEG(bytes: Uint8Array): Promise<Uint8Array | null> {
  if (bytes.byteLength === 0 || bytes.byteLength > previewLimit) return null;
  try {
    const heic2any = (await import("heic2any")).default;
    const result = await heic2any({
      blob: new Blob([bytes as BlobPart], { type: "image/heic" }),
      toType: "image/jpeg",
      quality: 0.82,
    });
    const blob = Array.isArray(result) ? result[0] : result;
    if (!blob) return null;
    return new Uint8Array(await blob.arrayBuffer());
  } catch {
    return null;
  }
}
