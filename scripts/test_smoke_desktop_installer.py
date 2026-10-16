import errno
import importlib.util
import os
from pathlib import Path
import shutil
import subprocess
import urllib.error
import unittest
from unittest.mock import MagicMock, patch

spec = importlib.util.spec_from_file_location('installer_smoke', Path(__file__).with_name('smoke-desktop-installer.py'))
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


class WebViewCloseTests(unittest.TestCase):
    def test_timeout_keeps_waiting_until_connection_is_refused(self):
        process = MagicMock(spec=['webview_port', 'pid', 'poll', 'terminate', 'kill', 'wait'])
        process.webview_port = 12345
        opener = MagicMock()
        opener.open.side_effect = [urllib.error.URLError(TimeoutError('slow endpoint')),
                                   urllib.error.URLError(ConnectionRefusedError(errno.ECONNREFUSED, 'closed'))]
        with patch.object(smoke.urllib.request, 'build_opener', return_value=opener), patch.object(smoke.time, 'sleep'):
            smoke.stop(process)
        self.assertEqual(opener.open.call_count, 2)

    def test_only_timeouts_fail_instead_of_crediting_an_old_webview(self):
        process = MagicMock(spec=['webview_port', 'pid', 'poll', 'terminate', 'kill', 'wait'])
        process.webview_port = 12345
        opener = MagicMock()
        opener.open.side_effect = urllib.error.URLError(TimeoutError('still listening'))
        with patch.object(smoke.urllib.request, 'build_opener', return_value=opener), patch.object(smoke.time, 'sleep'), patch.object(smoke.time, 'monotonic', side_effect=[0, 1, 31]):
            with self.assertRaisesRegex(RuntimeError, 'Old WebView remained running'):
                smoke.stop(process)


class WindowsInstallerContractTests(unittest.TestCase):
    def test_skill_protocol_registration_is_per_user_and_uninstall_checks_the_owner(self):
        text = (Path(__file__).parent / 'installers/windows.nsi').read_text(encoding='utf-8-sig')
        command = "'$\\\"$INSTDIR\\BeefTV.exe$\\\" $\\\"%1$\\\"'"
        self.assertIn('WriteRegStr HKCU "Software\\Classes\\beeftv" "URL Protocol" ""', text)
        self.assertIn('WriteRegStr HKCU "Software\\Classes\\beeftv\\shell\\open\\command" "" ' + command, text)
        uninstall = text.split('Section "Uninstall"', 1)[1]
        owner_check = '${If} $0 == ' + command
        self.assertIn('ReadRegStr $0 HKCU "Software\\Classes\\beeftv\\shell\\open\\command" ""', uninstall)
        guarded = uninstall.split(owner_check, 1)[1].split('${EndIf}', 1)[0]
        self.assertIn('DeleteRegKey HKCU "Software\\Classes\\beeftv"', guarded)
        self.assertEqual(text.count('DeleteRegKey HKCU "Software\\Classes\\beeftv"'), 1)

    def test_windows_nsi_uses_utf8_bom_and_streamlined_direct_install(self):
        nsi_path = Path(__file__).resolve().parent / 'installers/windows.nsi'
        raw = nsi_path.read_bytes()
        self.assertTrue(raw.startswith(b'\xef\xbb\xbf'), 'windows.nsi must start with a UTF-8 BOM')
        text = raw.decode('utf-8-sig')
        self.assertNotIn('MUI_PAGE_WELCOME', text)
        self.assertNotIn('MUI_PAGE_COMPONENTS', text)
        self.assertNotIn('StageDir', text)
        self.assertIn('!define MUI_FINISHPAGE_RUN "$INSTDIR\\BeefTV.exe"', text)
        self.assertIn('CreateShortcut "$DESKTOP\\BeefTV.lnk" "$INSTDIR\\BeefTV.exe"', text)
        self.assertIn('taskkill.exe', text)
        self.assertIn('Windows 无法移动以下程序文件夹', text)

    def test_packager_compiles_windows_installer_as_utf8_without_warnings(self):
        import os
        import shutil
        import subprocess
        import tempfile
        packager_path = Path(__file__).with_name('package-desktop-installer.py')
        packager_text = packager_path.read_text(encoding='utf-8')
        self.assertIn("'-INPUTCHARSET', 'UTF8'", packager_text)
        if not shutil.which('makensis'):
            return
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            payload = root / 'payload'
            payload.mkdir()
            (payload / 'BeefTV.exe').write_bytes(b'fixture')
            out_dir = root / 'dist'
            result = subprocess.run(
                ['python3', str(packager_path), '--platform', 'windows-amd64',
                 '--input', str(payload), '--output-dir', str(out_dir)],
                capture_output=True, text=True, check=True,
            )
            self.assertIn('(UTF8)', result.stdout)
            self.assertNotIn('warning', result.stdout.lower())
            self.assertEqual(len(list(out_dir.glob('BeefTV-*-windows-amd64-setup.exe'))), 1)


class NativeProtocolProcessTests(unittest.TestCase):
    def test_launcher_error_retains_os_reason_without_private_command_values(self):
        uri = 'beeftv://skills/install?id=private-skill&version=private-version'
        token = 'temporary-private-launch-token'
        root = '/tmp/private-workspace'
        result = subprocess.CompletedProcess([], 1, stderr=f'kLSApplicationNotFoundErr (-10814): {uri} HOME={root} CANVAS_DESKTOP_LAUNCH_TOKEN={token}\n'.encode())
        detail = smoke.protocol_launcher_diagnostic(result, [uri, token, root])
        self.assertIn('exit 1', detail)
        self.assertIn('kLSApplicationNotFoundErr (-10814)', detail)
        for private in [uri, token, root, 'private-skill']:
            self.assertNotIn(private, detail)
        self.assertNotIn('\n', detail)

    @unittest.skipIf(os.name == 'nt', 'Windows observer runs against the installed candidate in native CI')
    def test_observer_reads_the_exact_executable_pid(self):
        exe = Path(shutil.which('sleep')).resolve()
        process = subprocess.Popen([str(exe), '5'])
        try:
            self.assertIn(process.pid, smoke.native_app_pids(exe))
            self.assertEqual(smoke.native_app_pids(exe.with_name('not-the-candidate')), [])
        finally:
            process.terminate()
            process.wait(timeout=10)


if __name__ == '__main__':
    unittest.main()
