function crc32(data: Uint8Array): number {
  let c = ~0;
  for (const b of data) {
    c ^= b;
    for (let i = 0; i < 8; i++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1));
  }
  return ~c >>> 0;
}

function u16(n: number): number[] {
  return [n & 255, (n >> 8) & 255];
}

function u32(n: number): number[] {
  return [n & 255, (n >> 8) & 255, (n >> 16) & 255, (n >> 24) & 255];
}

export function zipStore(files: { name: string; data: Uint8Array }[]): Blob {
  const locals: Uint8Array[] = [];
  const central: number[] = [];
  let offset = 0;
  files.forEach((file, index) => {
    const name = new TextEncoder().encode(file.name || `photo-${index + 1}.jpg`);
    const crc = crc32(file.data);
    const local = [
      ...u32(0x04034b50),
      ...u16(20),
      ...u16(0x0800),
      ...u16(0),
      ...u16(0),
      ...u16(0),
      ...u32(crc),
      ...u32(file.data.length),
      ...u32(file.data.length),
      ...u16(name.length),
      ...u16(0),
      ...name,
    ];
    locals.push(new Uint8Array(local));
    locals.push(file.data);
    central.push(
      ...u32(0x02014b50),
      ...u16(20),
      ...u16(20),
      ...u16(0x0800),
      ...u16(0),
      ...u16(0),
      ...u16(0),
      ...u32(crc),
      ...u32(file.data.length),
      ...u32(file.data.length),
      ...u16(name.length),
      ...u16(0),
      ...u16(0),
      ...u16(0),
      ...u16(0),
      ...u32(0),
      ...u32(offset),
      ...name,
    );
    offset += local.length + file.data.length;
  });
  const centralBytes = new Uint8Array(central);
  const end = new Uint8Array([
    ...u32(0x06054b50),
    ...u16(0),
    ...u16(0),
    ...u16(files.length),
    ...u16(files.length),
    ...u32(centralBytes.length),
    ...u32(offset),
    ...u16(0),
  ]);
  const parts: BlobPart[] = [...locals, centralBytes, end] as BlobPart[];
  return new Blob(parts, { type: "application/zip" });
}
