import { describe, expect, it } from 'vitest';
import { fileProblem, progressText, sizeText } from './lora-import-model';

const MiB = 1 << 20;
const GiB = 1 << 30;

describe('the checks before a LoRA file is sent (#1119)', () => {
  it('accepts a .safetensors file within the limit, whatever the case of the extension', () => {
    expect(fileProblem({ name: 'a.safetensors', size: 10 * MiB }, 4 * GiB)).toBe('');
    expect(fileProblem({ name: 'A.SafeTensors', size: 1 }, 4 * GiB)).toBe('');
  });

  it('refuses any other extension, and says why a pickle file is not taken in', () => {
    expect(fileProblem({ name: 'a.ckpt', size: 10 }, GiB)).toContain('.safetensors のファイルを選んでください');
    expect(fileProblem({ name: 'a.pt', size: 10 }, GiB)).toContain('pickle');
    expect(fileProblem({ name: 'a.safetensors.exe', size: 10 }, GiB)).not.toBe('');
    expect(fileProblem({ name: 'noext', size: 10 }, GiB)).not.toBe('');
  });

  it('refuses an empty file and one over the limit', () => {
    expect(fileProblem({ name: 'a.safetensors', size: 0 }, GiB)).toBe('空のファイルは取り込めません');
    const big = fileProblem({ name: 'a.safetensors', size: 5 * GiB }, 4 * GiB);
    expect(big).toContain('ファイルが大きすぎます');
    expect(big).toContain('5.0 GiB');
    expect(big).toContain('4.0 GiB');
  });
});

describe('the progress of the transfer', () => {
  it('writes the part sent, and that the check follows once all of it is there', () => {
    expect(progressText(5 * MiB, 10 * MiB)).toBe('アップロード中 50%（5 MiB / 10 MiB）');
    expect(progressText(10 * MiB, 10 * MiB)).toBe('アップロードしました（10 MiB）。ファイルを確認しています…');
    expect(progressText(0, 0)).toBe('アップロードしています…');
  });

  it('writes sizes as the messages of the server do', () => {
    expect(sizeText(1536 * MiB)).toBe('1.5 GiB');
    expect(sizeText(100)).toBe('1 MiB');
  });
});
