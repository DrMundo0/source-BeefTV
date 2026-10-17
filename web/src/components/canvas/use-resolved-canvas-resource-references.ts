import { useEffect, useMemo, useState, useSyncExternalStore } from "react";

import type { CanvasResourceReference } from "@/lib/canvas/canvas-resource-references";
import { loadCanvasDrawingPreview } from "@/lib/canvas/canvas-drawing-storage";
import { resolveImageUrl } from "@/services/image-storage";
import { acquireCanvasVideoPreview, canvasDerivedPreviewSourceKey } from "@/services/canvas-video-preview";
import { captureUserScopeEpoch, getUserScopeGeneration, subscribeUserScope, userScopeEpochMatches } from "@/lib/user-scope";

type ResolvedPreview = {
    identity: string;
    url: string;
    derived: boolean;
};

const previewPromiseCache = new Map<string, Promise<string>>();
subscribeUserScope(() => previewPromiseCache.clear());

export function useResolvedCanvasResourceReferences(references: CanvasResourceReference[], options?: { projectId?: string; videoPreviewEnabled?: boolean }) {
    const projectId = options?.projectId;
    const videoPreviewEnabled = options?.videoPreviewEnabled;
    const scopeGeneration = useSyncExternalStore(subscribeUserScope, getUserScopeGeneration, getUserScopeGeneration);
    const requests = useMemo(
        () => references.flatMap((reference) => {
            const identity = previewIdentity(reference, projectId);
            return identity ? [{ reference, identity: `${scopeGeneration}:${identity}` }] : [];
        }),
        [projectId, references, scopeGeneration],
    );
    const [resolvedById, setResolvedById] = useState<Record<string, ResolvedPreview>>({});

    useEffect(() => {
        if (!requests.length) return;
        let cancelled = false;
        const epoch = captureUserScopeEpoch();
        const leases: ReturnType<typeof acquireCanvasVideoPreview>[] = [];
        const isCurrent = () => !cancelled && userScopeEpochMatches(epoch);
        requests.forEach(({ reference, identity }) => {
            void (async () => {
                let url = await resolveReferencePreview(reference, identity, projectId);
                let derived = false;
                if (!isCurrent()) return;
                // Lists include unused nodes and assets; decode only used videos
                // or thumbnails currently visible in the mention menu.
                if (!url && reference.videoPreviewSource && (videoPreviewEnabled ?? reference.active)) {
                    const lease = acquireCanvasVideoPreview(reference.videoPreviewSource);
                    leases.push(lease);
                    url = (await lease.promise)?.content || "";
                    derived = true;
                }
                if (!url || !isCurrent()) return;
                setResolvedById((current) => {
                    if (current[reference.id]?.identity === identity && current[reference.id]?.url === url) return current;
                    return { ...current, [reference.id]: { identity, url, derived } };
                });
            })();
        });
        return () => {
            cancelled = true;
            leases.forEach((lease) => lease.release());
        };
    }, [projectId, requests, videoPreviewEnabled]);

    return useMemo(
        () => references.map((reference) => {
            const key = previewIdentity(reference, projectId);
            const identity = key ? `${scopeGeneration}:${key}` : "";
            const resolved = identity ? resolvedById[reference.id] : undefined;
            if (resolved?.derived && !(videoPreviewEnabled ?? reference.active)) return reference;
            return resolved?.identity === identity && resolved.url !== reference.previewUrl ? { ...reference, previewUrl: resolved.url } : reference;
        }),
        [projectId, references, resolvedById, scopeGeneration, videoPreviewEnabled],
    );
}

function previewIdentity(reference: CanvasResourceReference, projectId?: string) {
    // 只有当 drawingId 和 projectId 同时存在时才返回绘图身份,避免不完整的缓存键导致运行时错误
    if (reference.drawingId) {
        if (!projectId) return "";
        return `drawing:${projectId}:${reference.drawingId}:${reference.drawingRevision || 0}`;
    }
    const storageKey = reference.kind === "video" ? reference.previewStorageKey : reference.storageKey;
    if (reference.kind === "video" && reference.videoPreviewSource) {
        return `video:${storageKey || ""}:${canvasDerivedPreviewSourceKey(reference.videoPreviewSource)}`;
    }
    if (!storageKey || !["image", "video", "character"].includes(reference.kind)) return "";
    return `${reference.kind}:${storageKey}`;
}

function resolveReferencePreview(reference: CanvasResourceReference, identity: string, projectId?: string) {
    if (reference.kind === "video" && !reference.previewStorageKey) return Promise.resolve(reference.previewUrl || "");
    const cached = previewPromiseCache.get(identity);
    if (cached) return cached;
    if (reference.drawingId && projectId) {
        const pending = loadCanvasDrawingPreview(projectId, reference.drawingId)
            .then((preview) => preview ? blobToDataUrl(preview) : reference.previewUrl || "")
            .catch(() => reference.previewUrl || "")
            .then((url) => {
                if (!url) previewPromiseCache.delete(identity);
                return url;
            });
        previewPromiseCache.set(identity, pending);
        return pending;
    }
    const storageKey = reference.kind === "video" ? reference.previewStorageKey : reference.storageKey;
    const pending = resolveImageUrl(storageKey, reference.previewUrl || "", { cacheMiss: true })
        .catch(() => reference.previewUrl || "")
        .then((url) => {
            if (!url) previewPromiseCache.delete(identity);
            return url;
        });
    previewPromiseCache.set(identity, pending);
    return pending;
}

function blobToDataUrl(blob: Blob) {
    return new Promise<string>((resolve, reject) => {
        const reader = new FileReader();
        reader.onerror = () => reject(reader.error || new Error("读取绘图预览失败"));
        reader.onload = () => resolve(typeof reader.result === "string" ? reader.result : "");
        reader.readAsDataURL(blob);
    });
}
