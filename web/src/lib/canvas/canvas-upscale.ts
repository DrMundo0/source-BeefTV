import type { CapturedUserScope } from "@/lib/user-scope-guard";
import type { UpscaleMode, UpscaleParams, UpscaleSource } from "@/services/api/media-upscale";

export type QuotedUpscaleSource = { nodeId: string; fingerprint: string; source: UpscaleSource; mode: UpscaleMode; reservation: number };
export type UpscaleQuote = { id: string; projectId: string; epoch: number; scope: CapturedUserScope; params: UpscaleParams; sources: QuotedUpscaleSource[]; reservation: number };
