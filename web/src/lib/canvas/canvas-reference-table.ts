/** Check explicit IMAGE table bindings against the final submitted image order. */
export function assertCanvasImageReferenceTable(prompt: string, imageTitles: readonly string[]) {
    const slotsByName = new Map<string, number | null>();
    imageTitles.forEach((name, index) => {
        const title = name.trim();
        if (!title) return;
        slotsByName.set(title, slotsByName.has(title) ? null : index + 1);
    });
    for (const row of prompt.matchAll(/^\s*\|\s*IMAGE\s+(\d+)\s*\|\s*`?@([^\s`|]+)`?/gimu)) {
        const declaredSlot = Number(row[1]);
        const numbered = /^图片(\d+)$/u.exec(row[2]);
        const actualSlot = numbered ? Number(numbered[1]) : slotsByName.get(row[2]);
        if (!actualSlot || actualSlot === declaredSlot || actualSlot > imageTitles.length) continue;
        throw new Error(`素材表中的 IMAGE ${declaredSlot} 对应图片${actualSlot}，编号不一致。请统一素材顺序和提示词编号后再生成`);
    }
}
