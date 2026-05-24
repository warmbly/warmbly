export function is64ByteHex(input: string): boolean {
  return /^[a-fA-F0-9]{128}$/.test(input);
}