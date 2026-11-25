package main

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	win32 "golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"motty/internal/buildinfo"
	"motty/internal/i18n"
)

//go:embed explorer_menu.ps1
var explorerPackageScript string

func explorerMenuCommand(args []string) (bool, error) {
	if len(args) != 1 || (args[0] != "--register-shell-menu" && args[0] != "--unregister-shell-menu") {
		return false, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return true, err
	}
	install := args[0] == "--register-shell-menu"
	build := win32.RtlGetVersion().BuildNumber
	if build >= 22000 {
		if err := configureModernExplorerMenu(exe, install); err != nil {
			// Keep an existing classic registration usable when deployment fails.
			return true, err
		}
	}
	label := "Open " + buildinfo.ProductName + " shell here"
	for _, tag := range preferredLanguages() {
		if strings.HasPrefix(strings.ToLower(tag), "zh") {
			label = "在此处打开 " + buildinfo.ProductName + " 终端"
			if i18n.ChineseVariant(tag) == i18n.TraditionalChinese {
				label = "在此處開啟 " + buildinfo.ProductName + " 終端機"
			}
			break
		}
		if strings.HasPrefix(strings.ToLower(tag), "en") {
			break
		}
	}
	for _, target := range []struct{ class, arg string }{{"Directory", "%1"}, {`Directory\Background`, "%V"}, {"Drive", "%1"}} {
		verb := `Software\Classes\` + target.class + `\shell\` + buildinfo.DataName + ".openHere"
		// Windows 11 resolves folders from the package. The packaged drive
		// verb arrived in 22H2, so 21H2 still needs its classic drive verb.
		classic := build < 22000 || (target.class == "Drive" && build < 22621)
		if !install || !classic {
			for _, key := range []string{verb + `\command`, verb} {
				if err := registry.DeleteKey(registry.CURRENT_USER, key); err != nil && !errors.Is(err, registry.ErrNotExist) {
					return true, err
				}
			}
			continue
		}
		// The dot protects the closing quote when Explorer supplies a drive root.
		command := fmt.Sprintf(`"%s" --local-dir "%s\."`, exe, target.arg)
		if err := registerExplorerVerb(verb, exe, label, command); err != nil {
			return true, err
		}
	}
	iconNotify.Call(0x08000000, 0, 0, 0) // SHCNE_ASSOCCHANGED
	return true, nil
}

// Appx is a Windows PowerShell module. Pass paths as process environment data
// so spaces, quotes and PowerShell expressions in a directory stay literal.
func configureModernExplorerMenu(exe string, install bool) error {
	system, err := win32.GetSystemDirectory()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	id := "Motty.ExplorerMenu"
	if buildinfo.Development {
		id = "Motty.Dev.ExplorerMenu"
	}
	action := "unregister"
	if install {
		action = "register"
		if !strings.EqualFold(filepath.Base(exe), buildinfo.DataName+".exe") {
			return fmt.Errorf("Explorer integration expects %s.exe", buildinfo.DataName)
		}
		menu := filepath.Join(dir, buildinfo.DataName+".explorer")
		if _, err := os.Stat(filepath.Join(menu, "OpenHere.dll")); err != nil {
			return fmt.Errorf("missing Windows 11 Explorer extension in %s (build with tools/explorermenu/build.ps1): %w", menu, err)
		}
	}
	cmd := exec.Command(filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", explorerPackageScript)
	cmd.Env = append(os.Environ(), "MOTTY_MENU_ROOT="+dir, "MOTTY_MENU_BASE="+buildinfo.DataName, "MOTTY_MENU_ID="+id, "MOTTY_MENU_ACTION="+action)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Windows Explorer menu: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func registerExplorerVerb(verb, exe, label, command string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, verb, registry.SET_VALUE|registry.CREATE_SUB_KEY)
	if err != nil {
		return err
	}
	defer key.Close()
	if err := key.SetStringValue("", label); err != nil {
		return err
	}
	if err := key.SetStringValue("Icon", fmt.Sprintf(`"%s",0`, exe)); err != nil {
		return err
	}
	child, _, err := registry.CreateKey(key, "command", registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer child.Close()
	return child.SetStringValue("", command)
}
