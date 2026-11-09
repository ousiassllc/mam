// The rules of the "ファイルを追加" form of the model manager (#1119) without the DOM: what the browser checks before it sends a LoRA file (the server checks
// everything again), and how the progress of the transfer is written. lora-import.ts draws them.

const MiB = 1 << 20;
const GiB = 1 << 30;

/** The size as a message writes it: GiB with a decimal, or whole MiB. */
export function sizeText(bytes: number): string {
  return bytes >= GiB ? `${(bytes / GiB).toFixed(1)} GiB` : `${Math.max(1, Math.round(bytes / MiB))} MiB`;
}

/** Why a chosen file cannot be sent ("" when it can): the extension, an empty file and the size limit. */
export function fileProblem(file: { name: string; size: number }, maxBytes: number): string {
  if (!file.name.toLowerCase().endsWith('.safetensors')) {
    return `.safetensors のファイルを選んでください（${file.name} は取り込めません。.ckpt・.pt などの pickle 形式は、読み込むときにプログラムが動く恐れがあるため取り込みません）`;
  }
  if (file.size === 0) return '空のファイルは取り込めません';
  if (maxBytes > 0 && file.size > maxBytes) {
    return `ファイルが大きすぎます（${sizeText(file.size)}。上限 ${sizeText(maxBytes)}）。LoRA ではなくチェックポイントなどの大きなモデルではありませんか`;
  }
  return '';
}

/** The line under the bar while a file is sent: the part sent, and that the check follows when all of it is there. */
export function progressText(loaded: number, total: number): string {
  if (total <= 0) return 'アップロードしています…';
  const percent = Math.min(100, Math.floor((loaded / total) * 100));
  if (loaded >= total) return `アップロードしました（${sizeText(total)}）。ファイルを確認しています…`;
  return `アップロード中 ${percent}%（${sizeText(loaded)} / ${sizeText(total)}）`;
}
