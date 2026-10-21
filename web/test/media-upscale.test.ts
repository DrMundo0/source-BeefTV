import { apiClient } from "../src/services/api/request";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";
import { canRecoverUpscaleNode, hasPendingUpscaleTask, upscaleObservationFailureMetadata, UPSCALE_OBSERVATION_TIMEOUT_MS } from "../src/lib/canvas/canvas-ai-upscale-task";
import { createModelChannel, modelOptionsFromChannels } from "../src/stores/use-config-store";
import { describe, expect, test } from "bun:test";
import { recoverUpscaleTask, validUpscaleScale } from "../src/services/api/media-upscale";
import { parseBackendGenerationResult } from "../src/services/api/generation-task";
import { generationTaskMode } from "../src/lib/canvas/canvas-generation-task-sync";
import type { GenerationTask } from "../src/services/api/task-center";

const task = (patch: Partial<GenerationTask>): GenerationTask => ({ id: "topaz", type: "media_upscale", status: "succeeded", prompt: "", attempts: 1, createdAt: "", updatedAt: "", ...patch });
describe("Topaz upscale contract", () => {
    test("failed upscale placeholder exposes recovery even without source media", () => {
        const node: CanvasNodeData = { id: "result", type: CanvasNodeType.Image, position: { x: 0, y: 0 }, width: 100, height: 100, metadata: { taskId: "original", taskStatus: "failed", status: "error", workflowOperation: { id: "image.ai-upscale", version: 1, config: {}, inputs: [] } } };
        expect(canRecoverUpscaleNode(node)).toBe(true);
        expect(canRecoverUpscaleNode({ ...node, metadata: { ...node.metadata, taskId: undefined } })).toBe(false);
        expect(canRecoverUpscaleNode({ ...node, metadata: { ...node.metadata, workflowOperation: undefined } })).toBe(false);
    });
    test("recovery calls only the original task recovery endpoint", async () => {
        const originalRequest = apiClient.request;
        const requests: Array<{ method?: string; url?: string; data?: unknown }> = [];
        apiClient.request = (async request => {
            requests.push({ method: request.method, url: request.url, data: request.data });
            return { status: 200, data: { code: 0, data: task({ id: "original", status: "running" }) } };
        }) as typeof apiClient.request;
        try {
            expect((await recoverUpscaleTask("original")).id).toBe("original");
            expect(requests).toEqual([{ method: "post", url: "/media-upscale/tasks/original/recover", data: {} }]);
        } finally { apiClient.request = originalRequest; }
    });
    test("observer timeout and network errors cannot authorize another paid task", () => {
        expect(UPSCALE_OBSERVATION_TIMEOUT_MS).toBeGreaterThan(10 * 60 * 1000);
        for (const status of ["queued", "running"] as const) {
            for (const reason of ["任务执行超时", "Network Error"]) {
                const metadata = upscaleObservationFailureMetadata({ id: "topaz", status }, reason);
                expect(metadata.status).toBe("loading");
                expect(metadata.taskStatus).toBe(status);
                expect(hasPendingUpscaleTask(metadata)).toBe(true);
                expect(hasPendingUpscaleTask({ ...metadata, status: "error" })).toBe(true);
            }
        }
        expect(hasPendingUpscaleTask({ taskId: "topaz", status: "error" })).toBe(true);
        expect(hasPendingUpscaleTask({ taskClientOperationId: "unknown", status: "error", generationSubmissionState: "unknown" })).toBe(true);
    });
    test("failed or cancelled tasks still require checking the original paid task", () => {
        for (const status of ["failed", "cancelled"] as const) {
            const metadata = upscaleObservationFailureMetadata({ id: "topaz", status }, "终态");
            expect(metadata.status).toBe("error");
            expect(hasPendingUpscaleTask(metadata)).toBe(true);
            expect(metadata.errorDetails).toContain("fal 控制台");
        }
        expect(hasPendingUpscaleTask({ taskClientOperationId: "not-submitted", taskStatus: "failed", generationSubmissionState: "rejected" })).toBe(false);
        expect(hasPendingUpscaleTask({ taskId: "paid-task", taskClientOperationId: "submitted", taskStatus: "failed", generationSubmissionState: "rejected" })).toBe(true);
        expect(hasPendingUpscaleTask({ taskClientOperationId: "unknown", taskStatus: "failed", generationSubmissionState: "unknown" })).toBe(true);
        const succeeded = upscaleObservationFailureMetadata({ id: "topaz", status: "succeeded" }, "落图失败");
        expect(hasPendingUpscaleTask(succeeded)).toBe(true);
        expect(hasPendingUpscaleTask({ ...succeeded, content: "resource-result" })).toBe(false);
    });
    test("processing models stay in settings but leave normal generation selectors", () => {
        const endpoints = ["topaz/upscale/image/precision", "topaz/upscale/image/generative", "topaz/upscale/image/transparent", "topaz/upscale/video/precision", "topaz/upscale/video/generative", "/topaz/upscale/image/precision"];
        const channel = createModelChannel({ id: "fal", models: [...endpoints, "fal-ai/flux/dev"] });
        expect(modelOptionsFromChannels([channel])).toEqual(["fal::fal-ai/flux/dev"]);
        expect(channel.models).toEqual([...endpoints, "fal-ai/flux/dev"]);
    });
    test("custom scale bounds and transparent fixed scale", () => {
        expect(validUpscaleScale(1.5)).toBe(true);
        for (const value of [0, 4.1, NaN, Infinity]) expect(validUpscaleScale(value)).toBe(false);
        expect(validUpscaleScale(2, 4)).toBe(false);
        expect(validUpscaleScale(4, 4)).toBe(true);
    });
    test("owned image result is recoverable without remote URL", () => {
        const result = parseBackendGenerationResult(task({ resultJson: JSON.stringify({ image: { resourceId: "image1", width: 200, height: 100 } }) }));
        expect(result.images?.[0].storageKey).toContain("image1");
        expect(result.images?.[0].width).toBe(200);
    });
    test("video recovery does not default to image", () => {
        expect(generationTaskMode(task({ inputJson: JSON.stringify({ mediaType: "video", mode: "precision" }) }))).toBe("video");
        const result = parseBackendGenerationResult(task({ resultJson: JSON.stringify({ video: { resourceId: "video1", width: 200, height: 100, durationMs: 1000 } }) }));
        expect(result.video?.durationMs).toBe(1000);
    });
});
