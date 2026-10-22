import { expect, test } from "bun:test";
import { assertCanvasImageReferenceTable } from "../src/lib/canvas/canvas-reference-table";

test("only rejects an unambiguous explicit IMAGE table binding", () => {
    const images = ["甲", "乙"];
    for (const prompt of ["| IMAGE 1 | `@乙` |", "| IMAGE 1 | @图片2 |", "| image 1 | @乙 |"])
        expect(() => assertCanvasImageReferenceTable(prompt, images)).toThrow("IMAGE 1 对应图片2");
    for (const prompt of ["| IMAGE 2 | `@乙` |", "| IMAGE 1 | @图片1 |", "IMAGE 1 在画面左，2 个人", "| IMAGE 1 | 未绑定名称 |", "| IMAGE 1 | @unknown |"])
        expect(() => assertCanvasImageReferenceTable(prompt, images)).not.toThrow();
    expect(() => assertCanvasImageReferenceTable("| IMAGE 1 | @同名 |", ["同名", "同名"])).not.toThrow();
});
