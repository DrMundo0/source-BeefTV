import { createRoot } from "react-dom/client";
import { App, ConfigProvider } from "antd";
import { ChannelSettingsPane } from "@/pages/settings/channel-settings-pane";
import { ModelDefaultGrid } from "@/pages/settings/model-default-grid";
import { getClientDiagnosticEvents } from "@/services/diagnostics/client-diagnostics";
import { hydrateModelConfig } from "@/services/model-config-repository";
import { createModelChannel, defaultConfig, normalizeConfigSnapshot, useConfigStore } from "@/stores/use-config-store";

const config = normalizeConfigSnapshot({
    config: {
        ...defaultConfig,
        channels: [createModelChannel({ id: "studio", name: "工作室渠道", baseUrl: "https://api.example.com", apiKey: "synthetic-key", models: ["chat-x", "image-y"], modelProfiles: [{ model: "chat-x", capability: "text", protocol: "chat-completion" }, { model: "image-y", capability: "image", protocol: "openai-image" }] })],
        textModel: "studio::chat-x",
    },
}).config;
useConfigStore.getState().replaceConfig(config);

(window as unknown as { diagnostics: () => unknown }).diagnostics = () => getClientDiagnosticEvents();

function Harness() {
    const live = useConfigStore((state) => state.config);
    return <ConfigProvider><App>
        <main style={{ padding: 40, width: 900 }}>
            <ChannelSettingsPane onOpenModels={() => {}} />
            <output data-testid="channel-count">{live.channels.length}</output>
            <ModelDefaultGrid config={config} onChange={() => {}} />
        </main>
    </App></ConfigProvider>;
}
void hydrateModelConfig().then(() => createRoot(document.getElementById("root")!).render(<Harness />));
