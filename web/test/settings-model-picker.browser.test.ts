import { afterAll, beforeAll, expect, test } from "bun:test";
import { existsSync } from "node:fs";
import { chromium, type Browser, type Page } from "playwright";

let browser: Browser;
let server: ReturnType<typeof Bun.serve>;

type DiagnosticEvent = { code?: string; message: string; level: string };

beforeAll(async () => {
    const build = await Bun.build({ entrypoints: [import.meta.dir + "/fixtures/settings-model-picker-harness.tsx"], target: "browser", define: { "import.meta.env.DEV": "false", "import.meta.env.PROD": "true", "import.meta.env.MODE": '"production"', "import.meta.env.VITE_CANVAS_LOCAL_MODE": '"true"', "import.meta.env.VITE_CANVAS_BACKEND_URL": '"/api"', "process.env.NODE_ENV": '"production"' }, plugins: [{ name: "source", setup(builder) {
        // Vite owns icon glob expansion; this harness only covers settings interactions.
        builder.onResolve({ filter: /^@\/components\/model-logo$/ }, () => ({ path: "model-logo", namespace: "test-icons" }));
        builder.onLoad({ filter: /.*/, namespace: "test-icons" }, () => ({ contents: "export function ModelLogo() { return null; }", loader: "js" }));
        builder.onResolve({ filter: /^@\// }, args => ({ path: Bun.resolveSync("../src/" + args.path.slice(2), import.meta.dir) }));
    } }] });
    if (!build.success) throw new Error(build.logs.join("\n"));
    const js = await build.outputs.find(o => o.path.endsWith(".js"))!.text();
    const css = await build.outputs.find(o => o.path.endsWith(".css"))?.text() || "";
    server = Bun.serve({ port: 0, fetch(req) {
        const path = new URL(req.url).pathname;
        if (path === "/harness.js") return new Response(js, { headers: { "Content-Type": "application/javascript" } });
        if (path === "/harness.css") return new Response(css, { headers: { "Content-Type": "text/css" } });
        if (path.includes("model-config")) return Response.json({ code: 0, data: req.method === "GET" ? { config: {}, revision: 0 } : { saved: true, revision: 1 } });
        if (path.startsWith("/api/")) return Response.json({ code: 0, data: {} });
        return new Response('<html><meta name="viewport" content="width=device-width"><link rel="stylesheet" href="/harness.css"><div id="root"></div><script type="module" src="/harness.js"></script></html>', { headers: { "Content-Type": "text/html" } });
    } });
    const executablePath = [process.env.CHROME_PATH, "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"].find((p): p is string => Boolean(p && existsSync(p)));
    browser = await chromium.launch({ executablePath, headless: true });
}, 60000);
afterAll(async () => { await browser?.close(); server?.stop(true); });

async function open(): Promise<Page> {
    const page = await browser.newPage({ viewport: { width: 1000, height: 800 } });
    await page.goto(server.url.toString());
    await page.getByRole("button", { name: "删除渠道 工作室渠道" }).waitFor();
    return page;
}

const events = (page: Page) => page.evaluate(() => (window as unknown as { diagnostics: () => DiagnosticEvent[] }).diagnostics());

test("删除渠道保持现有按钮与确认框，确认后渠道移除并留下诊断", async () => {
    const page = await open();
    try {
        const trigger = page.getByRole("button", { name: "删除渠道 工作室渠道" });
        await trigger.click();
        const ok = page.locator(".ant-popconfirm button").filter({ hasText: /^删\s?除$/ });
        await ok.waitFor();
        // The popconfirm is visible before its enter motion accepts pointer events.
        // Wait for hit testing, then use an actual pointer click at the button centre.
        const handle = await ok.elementHandle();
        await page.waitForFunction((button: Element) => {
            const box = button.getBoundingClientRect();
            const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2);
            return hit === button || button.contains(hit);
        }, handle, { timeout: 3000 });
        const box = (await ok.boundingBox())!;
        await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
        await page.getByTestId("channel-count").filter({ hasText: /^0$/ }).waitFor({ timeout: 3000 });
        await page.waitForFunction(() => (window as unknown as { diagnostics: () => DiagnosticEvent[] }).diagnostics().some(event => event.code === "CHANNEL_DELETED"), undefined, { timeout: 3000 });
        const codes = (await events(page)).map(event => event.code);
        expect(codes).toContain("CHANNEL_DELETE_CONFIRM_OPEN");
        expect(codes).toContain("CHANNEL_DELETED");
    } finally { await page.close(); }
}, 30000);

test("默认模型下拉展开时诊断记录点击、弹层位置与遮挡情况", async () => {
    const page = await open();
    try {
        await page.locator('section[aria-labelledby="default-text-title"] .model-default-select').click();
        await page.locator(".model-default-select-popup .ant-select-item-option").first().waitFor();
        await page.waitForFunction(() => (window as unknown as { diagnostics: () => DiagnosticEvent[] }).diagnostics().some(event => event.code === "MODEL_PICKER_OPEN" || event.code === "MODEL_PICKER_POPUP_HIDDEN"), undefined, { timeout: 3000 });
        const recorded = await events(page);
        expect(recorded.some(event => event.code === "MODEL_PICKER_PRESS" && event.message.includes("默认文本模型"))).toBe(true);
        const opened = recorded.find(event => event.code === "MODEL_PICKER_OPEN")!;
        expect(opened.message).toContain("未被遮挡");
        expect(opened.message).toContain("已渲染选项 1 个");
    } finally { await page.close(); }
}, 30000);

test("盖在下拉上的浮层接走点击时，诊断记下是谁挡住了", async () => {
    const page = await open();
    try {
        const select = page.locator(".model-default-select").first();
        const box = (await select.boundingBox())!;
        await page.evaluate(() => {
            const layer = document.createElement("div");
            layer.className = "ant-modal-mask";
            Object.assign(layer.style, { position: "fixed", inset: "0", zIndex: "2000" });
            document.body.appendChild(layer);
        });
        await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
        const blocked = (await events(page)).find(event => event.code === "MODEL_PICKER_BLOCKED");
        expect(blocked?.message).toContain("div.ant-modal-mask");
    } finally { await page.close(); }
}, 30000);
