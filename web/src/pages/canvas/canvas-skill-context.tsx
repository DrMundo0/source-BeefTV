import { Button, Tooltip } from "antd";
import { useEffect, useState } from "react";
import { http } from "@/services/api/request";
import { uiSessionConfig, type AssistantSkillSelection } from "@/services/api/agent-assistant";

type SkillContext = { scopeId: string; revision: number; activePins: AssistantSkillSelection[]; lifecycle: string };
export function CanvasSkillContext({ canvasId, sessionId, busy, readOnly, onChanged }: { canvasId: string; sessionId: string; busy: boolean; readOnly: boolean; onChanged: () => void }) {
    const [value, setValue] = useState<SkillContext | null>(null);
    const [error, setError] = useState("");
    const [pending, setPending] = useState(false);
    useEffect(() => {
        if (!sessionId.startsWith("durable:") || busy) return;
        let active = true;
        void uiSessionConfig()
            .then((config) => http.get<SkillContext>(`/assistant/canvases/${encodeURIComponent(canvasId)}/skill-context`, { ...config, params: { sessionId } }))
            .then((v) => {
                if (active) {
                    setValue(v);
                    setError("");
                }
            })
            .catch((e) => {
                if (active) setError(e instanceof Error ? e.message : "当前技能状态暂时无法读取");
            });
        return () => {
            active = false;
        };
    }, [canvasId, sessionId, busy]);
    const change = async (action: "refresh" | "clear" | "new_scope") => {
        if (!value) return;
        setPending(true);
        setError("");
        try {
            const v = await http.post<SkillContext>(`/assistant/canvases/${encodeURIComponent(canvasId)}/skill-context`, { sessionId, action, expectedRevision: value.revision, requestId: crypto.randomUUID() }, await uiSessionConfig());
            setValue(v);
            onChanged();
        } catch (e) {
            setError(e instanceof Error ? e.message : "技能状态未更新，请重试");
        } finally {
            setPending(false);
        }
    };
    if (!sessionId.startsWith("durable:")) return null;
    return (
        <div className="px-3 py-2 text-xs text-muted-foreground" aria-live="polite">
            <div className="flex flex-wrap items-center justify-between gap-1">
                <span>{value?.activePins?.length ? `当前使用 ${value.activePins.length} 个技能` : "当前没有活动技能"}</span>
                <div className="flex gap-1">
                    {Boolean(value?.activePins?.length) && (
                        <>
                            <Button size="small" type="text" disabled={busy || pending || readOnly} onClick={() => void change("refresh")}>
                                重新读取
                            </Button>
                            <Tooltip title="停止采用当前技能，仍可引用历史内容">
                                <Button size="small" type="text" disabled={busy || pending || readOnly} onClick={() => void change("clear")}>
                                    停用
                                </Button>
                            </Tooltip>
                        </>
                    )}
                    <Tooltip title="开始独立任务，助手不再沿用旧任务指令，历史记录会保留">
                        <Button size="small" type="text" disabled={!value || busy || pending || readOnly} onClick={() => void change("new_scope")}>
                            开始独立任务
                        </Button>
                    </Tooltip>
                </div>
            </div>
            {error && (
                <p role="alert" className="mt-1 text-destructive">
                    {error}
                </p>
            )}
        </div>
    );
}
