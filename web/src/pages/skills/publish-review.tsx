import { Alert, Button, Divider, Modal } from "antd";
import { useEffect, useState } from "react";
import { openPublishingAuthorization, publishingAuthorization, startPublishingAuthorization, submitPublication, uploadPublication, type PublishPlan } from "@/services/api/skill-market";

export function PublishReview({ plan, onClose, onChanged }: { plan: PublishPlan | null; onClose: () => void; onChanged: (plan: PublishPlan) => void }) {
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [authURL, setAuthURL] = useState("");
    useEffect(() => {
        setError("");
        setAuthURL("");
    }, [plan?.planId]);
    useEffect(() => {
        if (!authURL) return;
        let active = true;
        const t = window.setInterval(() => {
            void publishingAuthorization()
                .then((r) => {
                    if (active && r.connected) {
                        setAuthURL("");
                        setError("");
                    }
                })
                .catch((e) => { if (active) setError(e instanceof Error ? e.message : "投稿授权暂时无法确认，请重试"); });
        }, 5000);
        return () => {
            active = false;
            window.clearInterval(t);
        };
    }, [authURL]);
    const preview = plan?.preview;
    const sending = plan?.status === "awaiting_submit_confirmation";
    const finished = ["submitted", "in_review", "published"].includes(plan?.status || "");
    const act = async () => {
        if (!plan) return;
        setBusy(true);
        setError("");
        try {
            onChanged(await (sending ? submitPublication(plan) : uploadPublication(plan)));
        } catch (e) {
            setError(e instanceof Error ? e.message : "投稿暂时未完成，请重试");
        } finally {
            setBusy(false);
        }
    };
    return (
        <Modal
            open={Boolean(plan)}
            title={finished ? "投稿进度" : sending ? "确认公开内容并送审" : "确认上传这份技能"}
            onCancel={onClose}
            footer={
                <>
                    <Button onClick={onClose}>稍后处理</Button>
                    {!finished && (
                        <Button type="primary" loading={busy} onClick={() => void act()}>
                            {sending ? "确认送审" : "确认上传"}
                        </Button>
                    )}
                </>
            }
            destroyOnHidden
        >
            {plan && (
                <div className="space-y-4">
                    <p className="text-base font-semibold">{plan.metadata.name}</p>
                    <p className="text-muted-foreground">{plan.metadata.description}</p>
                    <p>作者 {preview?.author || "将使用授权账号的署名"}</p>
                    <p>
                        版本 {plan.metadata.versionLabel} · {plan.metadata.license}
                    </p>
                    <Alert type="info" showIcon title={sending ? "以下技能信息、文件和示例将在审核通过后公开。" : "以下文件将上传为私人稿件。上传后还需确认公开内容并送审。"} />
                    {(preview?.metadata || plan.metadata).sourceUrl && <p className="break-all">来源：{(preview?.metadata || plan.metadata).sourceUrl}</p>}
                    {(preview?.metadata || plan.metadata).requirements.length > 0 && <p>所需能力：{(preview?.metadata || plan.metadata).requirements.join("、")}</p>}
                    {(preview?.metadata || plan.metadata).minClientVersion && <p>最低应用版本：{(preview?.metadata || plan.metadata).minClientVersion}</p>}
                    {plan.instruction && (
                        <details>
                            <summary className="cursor-pointer">查看技能正文</summary>
                            <pre className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-lg border p-3 text-xs">{plan.instruction}</pre>
                        </details>
                    )}
                    <ul className="max-h-56 overflow-auto rounded-lg border p-3">
                        {(preview?.files || plan.files).map((f) => (
                            <li key={f.path} className="flex justify-between gap-4 py-1 text-sm">
                                <span className="min-w-0 break-all">{f.path}</span>
                                <span className="shrink-0 tabular-nums text-muted-foreground">{f.size.toLocaleString()} B</span>
                            </li>
                        ))}
                    </ul>
                    {preview?.metadata.showcase.map((item) => (
                        <a key={item.url} href={item.url} target="_blank" rel="noreferrer" className="block break-all">
                            {item.caption || "查看展示示例"}
                        </a>
                    ))}
                    {preview?.warnings?.map((w) => (
                        <Alert key={w} type="warning" title={w} />
                    ))}
                    {finished && <Alert type="success" title="已送审，可在我的创作查看后续状态。" />}
                    {error && (
                        <Alert
                            type="error"
                            title={error}
                            action={
                                <Button
                                    size="small"
                                    onClick={() =>
                                        void startPublishingAuthorization()
                                            .then(async (r) => {
                                                setAuthURL(r.verificationUrl);
                                                await openPublishingAuthorization(r.verificationUrl);
                                            })
                                            .catch((e) => setError(e instanceof Error ? e.message : "暂时无法连接账号"))
                                    }
                                >
                                    连接投稿账号
                                </Button>
                            }
                        />
                    )}
                    {authURL && (
                        <a href={authURL} target="_blank" rel="noreferrer">
                            打开账号授权页面，完成后重试
                        </a>
                    )}
                    <Divider className="!my-3" />
                    <p className="text-xs text-muted-foreground">画布、对话和其他本地素材不会随这份技能上传。</p>
                </div>
            )}
        </Modal>
    );
}
