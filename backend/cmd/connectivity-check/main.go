// connectivity-check performs bounded, unauthenticated network checks only.
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"infinite-canvas/backend/internal/desktopnet"
)

type target struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}
type configuration struct {
	Targets []target `json:"targets"`
}
type result struct {
	Target       string `json:"target"`
	Host         string `json:"host"`
	Route        string `json:"route"`
	Attempt      int    `json:"attempt"`
	ProxyUsed    bool   `json:"proxy_used"`
	Phase        string `json:"phase"`
	ErrorCode    string `json:"error_code,omitempty"`
	HTTPStatus   int    `json:"http_status,omitempty"`
	DNSMS        int64  `json:"dns_ms"`
	DNSAddresses int    `json:"dns_address_count"`
	ConnectMS    int64  `json:"connect_ms"`
	TLSMS        int64  `json:"tls_ms"`
	FirstByteMS  int64  `json:"first_byte_ms"`
	TotalMS      int64  `json:"total_ms"`
}
type report struct {
	Version        string   `json:"version"`
	CreatedAt      string   `json:"created_at"`
	OS             string   `json:"os"`
	Architecture   string   `json:"architecture"`
	ProxyMode      string   `json:"proxy_mode"`
	AutoConfigOnly bool     `json:"auto_config_only"`
	Results        []result `json:"results"`
}

func main() {
	executable, _ := os.Executable()
	configPath := flag.String("config", filepath.Join(filepath.Dir(executable), "targets.json"), "检测目标配置")
	outDir := flag.String("out", filepath.Dir(executable), "报告保存目录")
	pause := flag.Bool("pause", true, "完成后等待回车")
	flag.Parse()
	err := run(*configPath, *outDir)
	if err != nil {
		fmt.Println("检测未完成：", err)
	}
	if *pause {
		fmt.Println("按回车键关闭窗口。")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	if err != nil {
		os.Exit(1)
	}
}

func run(configPath, outDir string) error {
	config, err := loadConfiguration(configPath)
	if err != nil {
		return err
	}
	status := desktopnet.ConfigurationStatus()
	receipt := report{Version: "1", CreatedAt: time.Now().UTC().Format(time.RFC3339), OS: runtime.GOOS, Architecture: runtime.GOARCH, ProxyMode: status.Mode, AutoConfigOnly: status.AutoConfigOnly}
	fmt.Println("BeefTV 网络检测")
	fmt.Println("只检查网络连接，不生成内容、不扣生成费用、不读取密钥或素材。")
	fmt.Println("请保持与报错时相同的代理设置，检测通常需要 1 到 3 分钟。")
	for _, item := range config.Targets {
		for _, route := range []string{"beeftv", "direct"} {
			for attempt := 1; attempt <= 2; attempt++ {
				fmt.Printf("正在检测 %s，%s，第 %d 次……\n", item.Name, routeLabel(route), attempt)
				r := probe(context.Background(), item, route, attempt, desktopnet.Proxy)
				receipt.Results = append(receipt.Results, r)
				fmt.Println(resultLabel(r))
			}
		}
	}
	stamp := time.Now().Format("20060102-150405")
	base := filepath.Join(outDir, "BeefTV-network-report-"+stamp)
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return errors.New("无法整理检测结果")
	}
	if err := os.WriteFile(base+".json", data, 0600); err != nil {
		return errors.New("无法保存报告，请将工具放到可写入的文件夹后重试")
	}
	var readable strings.Builder
	fmt.Fprintf(&readable, "BeefTV 网络检测报告\r\n时间：%s\r\n系统：%s/%s\r\n代理来源：%s\r\n\r\n", receipt.CreatedAt, receipt.OS, receipt.Architecture, receipt.ProxyMode)
	if status.AutoConfigOnly {
		readable.WriteString("系统配置了自动代理；本次按 BeefTV 的当前处理方式检测，未执行 PAC/WPAD 脚本。\r\n")
	}
	for _, r := range receipt.Results {
		fmt.Fprintf(&readable, "%s | %s | 第%d次 | %s\r\n", r.Target, routeLabel(r.Route), r.Attempt, resultLabel(r))
	}
	readable.WriteString("\r\n连接成功只说明收到 HTTPS 响应。私有存储返回 403、未登录 API 返回 401 也表示网络已连通；本工具未验证素材上传或生成业务。\r\n“直连”仅表示不使用 HTTP 代理；系统 VPN/TUN 仍可能影响线路。\r\n报告不包含 API Key、代理地址、电脑名、公网 IP 或素材。\r\n")
	if err := os.WriteFile(base+".txt", []byte("\xef\xbb\xbf"+readable.String()), 0600); err != nil {
		return errors.New("JSON 已保存，文本报告保存失败")
	}
	fmt.Println("检测完成。请将同目录生成的报告 .txt 和 .json 发给客服：")
	fmt.Println(base + ".txt")
	return nil
}

func routeLabel(route string) string {
	if route == "direct" {
		return "不使用 HTTP 代理"
	}
	return "BeefTV 当前线路"
}
func resultLabel(r result) string {
	if r.ErrorCode != "" {
		return fmt.Sprintf("失败：%s，阶段=%s，耗时=%dms", r.ErrorCode, r.Phase, r.TotalMS)
	}
	return fmt.Sprintf("HTTPS 已连通，HTTP=%d，耗时=%dms，使用代理=%t", r.HTTPStatus, r.TotalMS, r.ProxyUsed)
}

func loadConfiguration(path string) (configuration, error) {
	var config configuration
	file, err := os.Open(path)
	if err != nil {
		return config, errors.New("找不到 targets.json，请先完整解压工具包")
	}
	defer file.Close()
	if err := json.NewDecoder(io.LimitReader(file, 16384)).Decode(&config); err != nil {
		return config, errors.New("检测目标配置无法读取")
	}
	if len(config.Targets) < 1 || len(config.Targets) > 4 {
		return config, errors.New("检测目标数量无效")
	}
	for _, t := range config.Targets {
		if err := validateTarget(t); err != nil {
			return config, err
		}
	}
	return config, nil
}

func validateTarget(t target) error {
	u, err := url.Parse(t.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") || (u.Path != "/" && u.Path != "/v1/videos" && u.Path != "/api/status") {
		return errors.New("检测目标必须是无凭据的 HTTPS 连通性地址")
	}
	if t.Name == "" || len(t.Name) > 80 || strings.ContainsAny(t.Name, "\r\n") {
		return errors.New("检测目标名称无效")
	}
	return nil
}

func probe(parent context.Context, item target, route string, attempt int, proxy func(*http.Request) (*url.URL, error)) result {
	u, _ := url.Parse(item.URL)
	r := result{Target: item.Name, Host: u.Hostname(), Route: route, Attempt: attempt, Phase: "prepare"}
	started := time.Now()
	var mu sync.Mutex
	var dns, connect, handshake time.Time
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { mu.Lock(); defer mu.Unlock(); r.Phase = "dns"; dns = time.Now() },
		DNSDone: func(info httptrace.DNSDoneInfo) {
			mu.Lock()
			defer mu.Unlock()
			r.DNSMS = time.Since(dns).Milliseconds()
			r.DNSAddresses = len(info.Addrs)
		},
		ConnectStart: func(string, string) { mu.Lock(); defer mu.Unlock(); r.Phase = "tcp"; connect = time.Now() },
		ConnectDone: func(_, _ string, err error) {
			mu.Lock()
			defer mu.Unlock()
			r.ConnectMS = time.Since(connect).Milliseconds()
			if err == nil {
				r.Phase = "proxy_or_tls"
			}
		},
		TLSHandshakeStart: func() { mu.Lock(); defer mu.Unlock(); r.Phase = "tls"; handshake = time.Now() },
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			mu.Lock()
			defer mu.Unlock()
			r.TLSMS = time.Since(handshake).Milliseconds()
		},
		GotConn: func(httptrace.GotConnInfo) { mu.Lock(); defer mu.Unlock(); r.Phase = "write_request" },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			mu.Lock()
			defer mu.Unlock()
			if info.Err == nil {
				r.Phase = "wait_response"
			}
		},
		GotFirstResponseByte: func() { mu.Lock(); defer mu.Unlock(); r.FirstByteMS = time.Since(started).Milliseconds() },
	}
	ctx, cancel := context.WithTimeout(httptrace.WithClientTrace(parent, trace), 25*time.Second)
	defer cancel()
	method := http.MethodHead
	if u.Path == "/v1/videos" || u.Path == "/api/status" {
		method = http.MethodGet
	}
	request, err := http.NewRequestWithContext(ctx, method, item.URL, nil)
	if err != nil {
		r.ErrorCode = "invalid_target"
		return r
	}
	request.Header.Set("User-Agent", "BeefTV-ConnectivityCheck/1")
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 10 * time.Second, ForceAttemptHTTP2: true, DisableKeepAlives: true}
	if route == "beeftv" {
		selected, proxyErr := proxy(request)
		if proxyErr != nil {
			r.ErrorCode = "proxy_configuration_error"
			return r
		}
		r.ProxyUsed = selected != nil
		transport.Proxy = http.ProxyURL(selected)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if response != nil {
		if err == nil {
			mu.Lock()
			r.Phase = "read_response"
			mu.Unlock()
			_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		}
		_ = response.Body.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	r.TotalMS = time.Since(started).Milliseconds()
	if response != nil {
		r.HTTPStatus = response.StatusCode
	}
	if err != nil {
		r.ErrorCode = networkErrorCode(err, r.Phase)
	} else {
		r.Phase = "http_response"
	}
	return r
}

func networkErrorCode(err error, phase string) string {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns_failed"
	}
	if strings.Contains(strings.ToLower(err.Error()), "certificate") {
		return "tls_certificate_error"
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout() {
		return phase + "_timeout"
	}
	return phase + "_failed"
}
