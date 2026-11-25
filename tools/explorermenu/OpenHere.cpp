#include <windows.h>
#include <shlobj.h>
#include <shlwapi.h>
#include <wrl.h>
#include <memory>
#include <string>
#include "MenuConfig.h"

using Microsoft::WRL::ComPtr;
using namespace Microsoft::WRL;
extern "C" IMAGE_DOS_HEADER __ImageBase;

// The package points to this DLL in the .explorer directory beside Motty.
static HRESULT menuDirectory(std::wstring& path) {
    path.resize(32768);
    DWORD used = GetModuleFileNameW(reinterpret_cast<HMODULE>(&__ImageBase), path.data(),
                                    static_cast<DWORD>(path.size()));
    if (!used || used == path.size()) return HRESULT_FROM_WIN32(GetLastError());
    path.resize(used);
    const auto slash = path.rfind(L'\\');
    if (slash == std::wstring::npos) return E_UNEXPECTED;
    path.resize(slash);
    return S_OK;
}

class __declspec(uuid(MOTTY_MENU_CLSID)) OpenHere final :
    public RuntimeClass<RuntimeClassFlags<ClassicCom | InhibitFtmBase>, IExplorerCommand, IObjectWithSite> {
    ComPtr<IUnknown> site_;

    HRESULT folder(IShellItemArray* items, IShellItem** result) const {
        *result = nullptr;
        if (items) {
            DWORD count = 0;
            HRESULT hr = items->GetCount(&count);
            if (FAILED(hr)) return hr;
            if (count > 1) return S_FALSE;
            if (count == 1) return items->GetItemAt(0, result);
        }
        if (!site_) return S_FALSE;
        ComPtr<IFolderView> view;
        HRESULT hr = IUnknown_QueryService(site_.Get(), SID_SFolderView, IID_PPV_ARGS(&view));
        return SUCCEEDED(hr) ? view->GetFolder(IID_PPV_ARGS(result)) : hr;
    }

public:
    IFACEMETHODIMP GetTitle(IShellItemArray*, PWSTR* title) override {
        LANGID lang = GetUserDefaultUILanguage();
        if (PRIMARYLANGID(lang) == LANG_CHINESE) {
            WORD sub = SUBLANGID(lang);
            if (sub == SUBLANG_CHINESE_TRADITIONAL || sub == SUBLANG_CHINESE_HONGKONG || sub == SUBLANG_CHINESE_MACAU)
                return SHStrDupW(L"在此處開啟 " MOTTY_MENU_PRODUCT L" 終端機", title);
            return SHStrDupW(L"在此处打开 " MOTTY_MENU_PRODUCT L" 终端", title);
        }
        return SHStrDupW(L"Open " MOTTY_MENU_PRODUCT L" shell here", title);
    }

    IFACEMETHODIMP GetIcon(IShellItemArray*, PWSTR* icon) override try {
        *icon = nullptr;
        std::wstring path;
        HRESULT hr = menuDirectory(path);
        if (FAILED(hr)) return hr;
        return SHStrDupW((L"\"" + path + L"\\menu.ico\",0").c_str(), icon);
    } catch (const std::bad_alloc&) { return E_OUTOFMEMORY; }

    IFACEMETHODIMP GetState(IShellItemArray* items, BOOL, EXPCMDSTATE* state) override {
        *state = ECS_HIDDEN;
        ComPtr<IShellItem> item;
        if (folder(items, &item) == S_OK && item) {
            constexpr SFGAOF required = SFGAO_FILESYSTEM | SFGAO_FOLDER;
            SFGAOF attr = 0;
            if (SUCCEEDED(item->GetAttributes(required | SFGAO_STREAM, &attr)) && attr == required)
                *state = ECS_ENABLED;
        }
        return S_OK;
    }

    IFACEMETHODIMP Invoke(IShellItemArray* items, IBindCtx*) override try {
        ComPtr<IShellItem> item;
        HRESULT hr = folder(items, &item);
        if (hr != S_OK || !item) return hr;
        PWSTR name = nullptr;
        hr = item->GetDisplayName(SIGDN_FILESYSPATH, &name);
        if (FAILED(hr)) return hr;
        std::unique_ptr<wchar_t, decltype(&CoTaskMemFree)> dir(name, CoTaskMemFree);
        std::wstring exe;
        hr = menuDirectory(exe);
        if (FAILED(hr)) return hr;
        const auto slash = exe.rfind(L'\\');
        if (slash == std::wstring::npos) return E_UNEXPECTED;
        exe.replace(slash + 1, std::wstring::npos, MOTTY_MENU_EXE);
        // The dot protects the closing quote for drive and UNC roots.
        std::wstring args = L"\"" + exe + L"\" --local-dir \"" + dir.get();
        if (args.back() != L'\\') args += L'\\';
        args += L".\"";
        STARTUPINFOW startup{sizeof(startup)};
        PROCESS_INFORMATION process{};
        if (!CreateProcessW(exe.c_str(), args.data(), nullptr, nullptr, FALSE, 0, nullptr, nullptr, &startup, &process))
            return HRESULT_FROM_WIN32(GetLastError());
        CloseHandle(process.hThread);
        CloseHandle(process.hProcess);
        return S_OK;
    } catch (const std::bad_alloc&) { return E_OUTOFMEMORY; }

    IFACEMETHODIMP SetSite(IUnknown* site) override { site_ = site; return S_OK; }
    IFACEMETHODIMP GetSite(REFIID iid, void** result) override {
        *result = nullptr;
        return site_ ? site_->QueryInterface(iid, result) : E_FAIL;
    }
    IFACEMETHODIMP GetCanonicalName(GUID* name) override { *name = __uuidof(OpenHere); return S_OK; }
    IFACEMETHODIMP GetFlags(EXPCMDFLAGS* flags) override { *flags = ECF_DEFAULT; return S_OK; }
    IFACEMETHODIMP GetToolTip(IShellItemArray*, PWSTR* tip) override { *tip = nullptr; return E_NOTIMPL; }
    IFACEMETHODIMP EnumSubCommands(IEnumExplorerCommand** commands) override { *commands = nullptr; return E_NOTIMPL; }
};

CoCreatableClass(OpenHere);

STDAPI DllGetClassObject(REFCLSID clsid, REFIID iid, void** result) {
    return Module<InProc>::GetModule().GetClassObject(clsid, iid, result);
}

STDAPI DllCanUnloadNow() {
    return Module<InProc>::GetModule().Terminate() ? S_OK : S_FALSE;
}
