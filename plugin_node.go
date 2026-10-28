package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func nodePermissionArgs(manifest *pluginManifest, pluginRoot, workspaceRoot, entryPath string) []string {
	return nodePermissionArgsWithData(manifest, pluginRoot, workspaceRoot, entryPath, "")
}

func nodePermissionArgsWithData(
	manifest *pluginManifest,
	pluginRoot, workspaceRoot, entryPath, dataDir string,
) []string {
	args := []string{
		"--permission",
		"--allow-fs-read=" + pluginRoot,
	}
	if dataDir != "" {
		args = append(args,
			"--allow-fs-read="+dataDir,
			"--allow-fs-write="+dataDir,
		)
	}
	if pluginFilesReadable(manifest) {
		args = append(args, "--allow-fs-read="+workspaceRoot)
	}
	if pluginFilesWritable(manifest) {
		args = append(args, "--allow-fs-write="+workspaceRoot)
	}
	if manifest.Capabilities.Network {
		args = append(args, "--allow-net")
	}
	if manifest.Capabilities.Process == "execute" {
		args = append(args, "--allow-child-process")
	}
	return append(args, entryPath)
}

func (h *pluginHost) nodeRuntime() (string, error) {
	h.nodeMu.Lock()
	defer h.nodeMu.Unlock()
	if h.nodeChecked {
		return h.nodePath, h.nodeErr
	}
	h.nodeChecked = true

	binary := strings.TrimSpace(os.Getenv("BYOP_NODE"))
	if binary == "" {
		resolved, err := exec.LookPath("node")
		if err != nil {
			h.nodeErr = errorsNodeNotFound("executable was not found in PATH")
			return "", h.nodeErr
		}
		binary = resolved
	} else if !strings.ContainsRune(binary, '/') {
		if resolved, err := exec.LookPath(binary); err == nil {
			binary = resolved
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, binary, "--version").CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		h.nodeErr = errorsNodeNotFound(detail)
		return "", h.nodeErr
	}
	version := strings.TrimSpace(string(output))
	majorText := strings.TrimPrefix(version, "v")
	if dot := strings.IndexByte(majorText, '.'); dot >= 0 {
		majorText = majorText[:dot]
	}
	major, parseErr := strconv.Atoi(majorText)
	if parseErr != nil || major < 25 {
		h.nodeErr = errorsNodeNotFound("found " + version)
		return "", h.nodeErr
	}
	h.nodePath = binary
	h.nodeVersion = version
	return binary, nil
}

func errorsNodeNotFound(detail string) error {
	return fmt.Errorf("Node.js ≥25 not found: %s", detail)
}
