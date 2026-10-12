import { formatBytes } from "@/lib/image-utils";
import type { GenerationTaskDeliveryProgress } from "@/services/api/task-center";

export type GenerationDeliveryProgressView = { title: string; percent: number | null; detail: string };

function sizeLabel(bytes: number) {
    return formatBytes(bytes) || "0 B";
}

export function generationDeliveryProgressView(progress: GenerationTaskDeliveryProgress): GenerationDeliveryProgressView {
    const downloaded = Math.max(0, progress.downloadedBytes);
    const known = progress.totalBytes > 0;
    const percent = known ? Math.min(100, Math.floor((downloaded * 100) / progress.totalBytes)) : null;
    const size = known ? `${sizeLabel(downloaded)} / ${sizeLabel(progress.totalBytes)}` : sizeLabel(downloaded);
    const retrying = progress.state === "retrying";
    return {
        title: retrying ? "下载中断，正在续传" : "正在取回结果",
        percent,
        detail: [size, retrying || progress.bytesPerSecond <= 0 ? "" : `${sizeLabel(progress.bytesPerSecond)}/s`].filter(Boolean).join(" · "),
    };
}
