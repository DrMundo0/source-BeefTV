import { expect, test } from "bun:test";

import { generationDeliveryProgressView } from "@/lib/generation-delivery-progress";

test("取回进度显示百分比、已下载/总大小和速度", () => {
    expect(generationDeliveryProgressView({ outputIndex: 0, downloadedBytes: 12_900_000, totalBytes: 41_943_040, bytesPerSecond: 1_258_291, state: "downloading" })).toEqual({
        title: "正在取回结果",
        percent: 30,
        detail: "12 MB / 40 MB · 1.2 MB/s",
    });
});

test("总大小未知时只显示已下载量，中断续传时不显示速度", () => {
    expect(generationDeliveryProgressView({ outputIndex: 0, downloadedBytes: 2048, totalBytes: -1, bytesPerSecond: 0, state: "retrying" })).toEqual({
        title: "下载中断，正在续传",
        percent: null,
        detail: "2.0 KB",
    });
});
