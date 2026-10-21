import { expect, test } from "bun:test";
import { canSubmitPublication, requirementLabel, publicationStatus } from "../src/pages/skills/market-copy";

async function source(path: string) {
    return Bun.file(new URL(path, import.meta.url)).text();
}

test("implemented skill route is discoverable while reference experiments remain hidden", async () => {
    const [router, sidebar] = await Promise.all([
        source("../src/router.tsx"),
        source("../src/components/layout/workspace-sidebar-nav.tsx"),
    ]);
    expect(router).toContain('{ path: "/skill", element: <Navigate to="/skills" replace /> }');
    expect(router).toContain('{ path: "/skills", element: deferred(<SkillsPage />) }');
    expect(router).not.toContain("SkillsReferencePage");
    expect(sidebar).toContain('toolItem("skills", "/skills")');
});

test("public submission requires this preview and a separate user confirmation", () => {
    for (const status of ["awaiting_upload_confirmation", "uploading", "needs_preview", "submitted", "published", "rejected"]) {
        expect(canSubmitPublication({ status, preview: { revision: 2 } }, true)).toBe(false);
    }
    expect(canSubmitPublication({ status: "awaiting_submit_confirmation" }, true)).toBe(false);
    expect(canSubmitPublication({ status: "awaiting_submit_confirmation", preview: {} }, false)).toBe(false);
    expect(canSubmitPublication({ status: "awaiting_submit_confirmation", preview: {} }, true)).toBe(true);
});

test("unknown capabilities and publication states never look accepted", () => {
    expect(requirementLabel("model.audio")).toBe("音频理解模型");
    expect(requirementLabel("needs secret")).toContain("未确认");
    expect(publicationStatus("new-unknown-state")).toBe("状态待刷新");
    expect(publicationStatus("rejected")).toBe("需要修改");
});
