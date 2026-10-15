import { http } from "@/services/api/request";
import { recordDiagnosticEvent } from "./client-diagnostics";
import { browserChecks, probeBrowserStorage } from "./browser-environment";

export type EnvironmentCheck = { id: string; title: string; status: "pass" | "warning" | "error" | "repaired" | "skipped" | "unknown"; detail: string; cases: string[] };
export type EnvironmentReport = { checkedAt: string; os: string; arch: string; catalog: { repository: string; revision: string; cases: { id: string; title: string; status: string }[] }; checks: EnvironmentCheck[] };

export { browserChecks, browserCapabilities } from "./browser-environment";

export async function checkEnvironment(repair = false): Promise<EnvironmentReport> {
    return http.post<EnvironmentReport>("/diagnostics/environment", { repair });
}

// One non-blocking attempt per authenticated workspace. Failures remain visible
// in diagnostics; startup never waits for repairs or retries in a loop.
export async function runStartupEnvironmentCheck() {
    try {
        const [native] = await Promise.allSettled([checkEnvironment(true), probeBrowserStorage()]);
        const report: EnvironmentReport = native.status === "fulfilled" ? native.value : {
            checkedAt: new Date().toISOString(), os: "browser", arch: "unknown", catalog: { repository: "", revision: "", cases: [] },
            checks: [{ id: "check-unavailable", title: "本地服务检查", status: "unknown", detail: "启动环境检查未完成，可在问题诊断中重新检查。", cases: [] }],
        };
        const checks = [...report.checks, ...browserChecks()];
        for (const check of checks) {
            if (check.status === "pass" || check.status === "skipped") continue;
            recordDiagnosticEvent({ category: "environment", code: check.id, level: check.status === "error" ? "error" : check.status === "repaired" ? "info" : "warning", message: check.detail });
        }
        return { ...report, checks };
    } catch {
        recordDiagnosticEvent({ category: "environment", code: "check-unavailable", level: "warning", message: "启动环境检查未完成，可在问题诊断中重新检查。" });
        return undefined;
    }
}
