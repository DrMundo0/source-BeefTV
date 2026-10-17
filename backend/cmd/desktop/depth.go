package main

import (
	"context"
	"errors"
	"infinite-canvas/backend/internal/depthcapture"
	"time"
)

func (a *DesktopApp) DepthComponentStatus(refresh bool) (depthcapture.ComponentState, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	runtime := a.runtime()
	if runtime == nil || a.migrating || a.closing {
		return depthcapture.ComponentState{}, errors.New("工作区正在关闭或迁移，请稍后再检查组件")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return runtime.DepthComponents().ComponentStatus(ctx, refresh)
}

func (a *DesktopApp) DownloadDepthComponents() (depthcapture.ComponentState, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	runtime := a.runtime()
	if runtime == nil || a.migrating || a.closing {
		return depthcapture.ComponentState{}, errors.New("工作区正在关闭或迁移，请稍后再下载")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return runtime.DepthComponents().StartComponentDownload(ctx)
}

func (a *DesktopApp) PauseDepthComponentDownload() (depthcapture.ComponentState, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	runtime := a.runtime()
	if runtime == nil || a.migrating || a.closing {
		return depthcapture.ComponentState{}, errors.New("工作区正在关闭或迁移，请稍后再暂停下载")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := runtime.DepthComponents().PauseComponentDownload(ctx); err != nil {
		return depthcapture.ComponentState{}, err
	}
	return runtime.DepthComponents().ComponentStatus(ctx)
}
