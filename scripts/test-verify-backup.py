"""文件作用说明：验证备份恢复工具拒绝业务库名与恶意归档，防止误删运行数据库或解包越界。"""
import io
import runpy
import tarfile
import tempfile
import unittest
from pathlib import Path

HELPERS = runpy.run_path(str(Path(__file__).with_name("verify-backup.py")))


class BackupSafetyTests(unittest.TestCase):
    """覆盖清理库名和不可信归档边界，不连接Docker或真实数据库。"""

    def test_reject_running_or_injected_database_names(self):
        """运行库、系统库和带引号的注入名称不得进入清理SQL。"""
        for value in ["goodchat_saas", "postgres", "goodchat_restore_check_", 'x";DROP DATABASE goodchat_saas;--']:
            with self.assertRaises(ValueError):
                HELPERS["temporary_name"](value)
        value = "goodchat_restore_check_" + "a" * 24
        self.assertEqual(HELPERS["temporary_name"](value), value)

    def test_reject_links_traversal_and_duplicate_archive_members(self):
        """越界、符号链接及重复文件即使带有效密钥也必须拒绝。"""
        with tempfile.TemporaryDirectory() as folder:
            for kind in ["traversal", "link", "duplicate"]:
                path = Path(folder) / (kind + ".tar.gz")
                with tarfile.open(path, "w:gz") as archive:
                    key = tarfile.TarInfo("auth-secret"); key.size = 32
                    archive.addfile(key, io.BytesIO(b"x" * 32))
                    item = tarfile.TarInfo("../outside" if kind == "traversal" else "auth-secret" if kind == "duplicate" else "container-data/link")
                    if kind == "link":
                        item.type = tarfile.SYMTYPE; item.linkname = "/etc/passwd"
                    archive.addfile(item, io.BytesIO())
                with self.assertRaises(ValueError):
                    HELPERS["archive_files"](path)


if __name__ == "__main__":
    unittest.main()
