import { useState } from "react";
import { createRoot } from "react-dom/client";
import { App, ConfigProvider, theme } from "antd";
import { ModelServiceEditor } from "../../src/pages/settings/model-service-editor";
import { createModelChannel, type ModelChannel } from "../../src/stores/use-config-store";

function Harness() {
    const [saved, setSaved] = useState<ModelChannel>();
    const dark = new URLSearchParams(location.search).get("theme") === "dark";
    return <ConfigProvider theme={{ algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm }}><App>
        <ModelServiceEditor initial={createModelChannel({ name: "外接服务", baseUrl: "https://video.example.com/v1", apiKey: "fixture-key", models: [] })} onClose={() => {}} onSave={async (channel) => { setSaved(channel); }} />
        <output data-testid="saved-profile">{saved?.modelProfiles?.[0]?.protocol || "unsaved"}</output>
    </App></ConfigProvider>;
}
createRoot(document.getElementById("root")!).render(<Harness />);
