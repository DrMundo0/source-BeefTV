import { expect, test } from "bun:test";
import { browserChecks } from "../src/services/diagnostics/browser-environment";

test("missing randomUUID is compatible while missing secure randomness is actionable", () => {
    const capabilities = { secureRandom: true, randomUUID: false, structuredClone: true, indexedDB: true, webGL: true, h264: true, hevc: false };
    expect(browserChecks(capabilities).find(item => item.id === "secure-random")?.status).toBe("pass");
    expect(browserChecks({ ...capabilities, secureRandom: false }).find(item => item.id === "secure-random")?.status).toBe("error");
    expect(browserChecks(capabilities).find(item => item.id === "video-codec")?.status).toBe("pass");
});

test("missing browser APIs are independent of optional GPU and HEVC capabilities", () => {
    const checks = browserChecks({ secureRandom: true, randomUUID: true, structuredClone: false, indexedDB: false, webGL: false, h264: false, hevc: false });
    expect(checks.filter(item => item.status === "error").map(item => item.id)).toEqual(["structured-clone", "indexed-db"]);
    expect(checks.filter(item => item.status === "warning").map(item => item.id)).toEqual(["webgl", "video-codec"]);
});
