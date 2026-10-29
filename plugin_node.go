package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// nodeRuntime finds the Node.js executable the plugin runtime runs. A window
// started from Finder or Launchpad inherits a short PATH without the package
// manager folders, so the search follows the same order as the Pi CLI: the
// explicit setting, this process PATH, the common install folders, then the
// login shell PATH. Only a resolved runtime is cached, so a plugin retried
// after a failed start can find Node once the login probe succeeds.
func (h *pluginHost) nodeRuntime() (string, error) {
	h.nodeMu.Lock()
	defer h.nodeMu.Unlock()
	if h.nodeChecked {
		return h.nodePath, nil
	}

	binary := strings.TrimSpace(os.Getenv("BYOP_NODE"))
	switch {
	case binary == "":
		resolved, err := resolveNodeCommand("node")
		if err != nil {
			h.nodeErr = errorsNodeNotFound(
				"executable was not found in PATH or the common Node.js folders")
			return "", h.nodeErr
		}
		binary = resolved
	case !strings.ContainsRune(binary, '/'):
		// A bare setting is a command name: keep it as written when it cannot
		// be found so the version check reports what the user set.
		if resolved, err := resolveNodeCommand(binary); err == nil {
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
	h.nodeChecked = true
	return binary, nil
}

// resolveNodeCommand looks for a Node.js command in this process PATH first,
// then in the folders where Node.js is commonly installed, and last in the
// login shell PATH.
func resolveNodeCommand(command string) (string, error) {
	if resolved, err := exec.LookPath(command); err == nil {
		return resolved, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if found := findExecutableInDirectories(command, piCommonInstallDirectories()); found != "" {
		return found, nil
	}
	if found := findExecutableInDirectories(command, filepath.SplitList(cachedPiLoginShellPATH(ctx))); found != "" {
		return found, nil
	}
	return "", exec.ErrNotFound
}

func errorsNodeNotFound(detail string) error {
	return fmt.Errorf("Node.js ≥25 not found: %s", detail)
}
