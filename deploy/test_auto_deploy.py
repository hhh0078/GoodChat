"""文件作用说明：验证部署清理只删除GoodChat构建产物，不删除有容器引用的镜像或其他项目资源。"""
import importlib.util
from pathlib import Path
from io import BytesIO
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("goodchat_deploy", Path(__file__).with_name("auto-deploy.py"))
deploy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(deploy)


class CleanupImagesTest(unittest.TestCase):
    """覆盖清理标签、镜像命名和容器引用这三个删除边界。"""

    def test_only_unused_owned_images_are_removed(self):
        """构建层可回收，活动和回退镜像、手动标签与其他项目必须保留。"""
        def inventory(args, root, capture=False):
            """提供隔离镜像清单，拒绝测试未预期的Docker查询。"""
            if args[:3] == ["docker", "image", "ls"]:
                if "--filter" in args:
                    self.assertIn("label=cn.goodchat.build-stage=true", args)
                    return "build-stage\n"
                return "goodchat:111111111111\ngoodchat:222222222222\ngoodchat:333333333333\ngoodchat:manual\nother:444444444444\n"
            self.assertEqual(args[:4], ["docker", "ps", "-aq", "--filter"])
            return "container-id" if args[-1] in ["ancestor=goodchat:111111111111", "ancestor=goodchat:222222222222"] else ""

        with patch.object(deploy, "run", side_effect=inventory), patch.object(deploy.subprocess, "run") as remove:
            deploy.cleanup_images(Path("."))
        removed = [call.args[0] for call in remove.call_args_list]
        self.assertEqual(removed, [["docker", "image", "rm", "build-stage"], ["docker", "image", "rm", "goodchat:333333333333"]])


class SitemapSnapshotTest(unittest.TestCase):
    """覆盖静态文件更新、上游故障及异常目录不能覆盖有效地图的边界。"""

    def setUp(self):
        """创建隔离目录与九语言目录样本，不访问线上服务。"""
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name) / "app"
        self.target = self.root.parent / ".well-known" / "sitemap.xml"
        self.target.parent.mkdir()
        self.target.write_bytes(b"previous-valid-snapshot")
        self.content = ('<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">' + ''.join(
            f'<url><loc>https://goodchat.58it.cn/en/articles/example-{i}</loc></url>' for i in range(81)
        ) + '</urlset>').encode()
        self.origin_patch = patch.object(deploy, "public_origins", return_value=["https://goodchat.58it.cn"])
        self.origin_patch.start()
        self.addCleanup(self.origin_patch.stop)
        self.key_patch = patch.object(deploy, "refresh_indexnow_key")
        self.key_patch.start()
        self.addCleanup(self.key_patch.stop)

    def response(self, content):
        """模拟已完整生成的本机HTTP响应，不参与真实业务验收。"""
        response = BytesIO(content)
        response.status = 200
        return response

    def test_atomic_update_and_unchanged_mtime(self):
        """有效新目录替换旧文件，相同内容不改变Last-Modified依据。"""
        with patch.object(deploy.urllib.request, "urlopen", side_effect=lambda *a, **k: self.response(self.content)):
            deploy.refresh_sitemap(self.root)
            stamp = self.target.stat().st_mtime_ns
            deploy.refresh_sitemap(self.root)
        self.assertEqual(self.target.read_bytes(), self.content)
        self.assertEqual((self.target.parent / 'sitemap-crawl-check-20261120.xml').read_bytes(), self.content)
        self.assertEqual(self.target.stat().st_mtime_ns, stamp)
        self.assertEqual(self.target.stat().st_mode & 0o777, 0o644)
        self.assertEqual(list(self.target.parent.glob('.sitemap-*')), [])

    def test_invalid_or_incomplete_maps_keep_previous(self):
        """错误页面、缺项、重复项及外站地址均不能污染公开地图。"""
        cases = [b'<html/>', self.content.replace(b'goodchat.58it.cn', b'other.example'),
                 self.content.replace(b'example-1<', b'example-2<'),
                 b'<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"/>']
        for content in cases:
            with self.subTest(content=content[:30]), patch.object(deploy.urllib.request, "urlopen", return_value=self.response(content)):
                deploy.try_refresh_sitemap(self.root)
                self.assertEqual(self.target.read_bytes(), b'previous-valid-snapshot')

    def test_unavailable_upstream_keeps_previous(self):
        """网络中断后继续提供已发布地图，不写入半份响应。"""
        with patch.object(deploy.urllib.request, "urlopen", side_effect=TimeoutError):
            deploy.try_refresh_sitemap(self.root)
        self.assertEqual(self.target.read_bytes(), b'previous-valid-snapshot')

    def test_alias_has_separate_snapshot(self):
        """每个站点使用对应Host生成独立文件，默认地图保持主站且拒绝串站内容。"""
        self.origin_patch.stop()
        alias = self.content.replace(b'goodchat.58it.cn', b'goodchat.ai.58it.cn')
        def fetch(request, **kwargs):
            """按实际Host返回对应目录，防止实现只复制主站地图。"""
            return self.response(alias if request.get_header('Host') == 'goodchat.ai.58it.cn' else self.content)
        with patch.object(deploy,'public_origins',return_value=['https://goodchat.58it.cn','https://goodchat.ai.58it.cn']), patch.object(deploy.urllib.request,'urlopen',side_effect=fetch):
            deploy.refresh_sitemap(self.root)
        self.assertEqual(self.target.read_bytes(),self.content)
        self.assertEqual((self.target.parent/'sitemap-goodchat.ai.58it.cn.xml').read_bytes(),alias)
        self.assertEqual((self.target.parent/'sitemap-goodchat.58it.cn.xml').read_bytes(),self.content)

    def test_public_key_sync_and_disabled_removal(self):
        """验证键只接受规范格式，明确404时删除已停用副本。"""
        self.key_patch.stop()
        key_path = self.target.parent / 'indexnow-key.txt'
        with patch.object(deploy.urllib.request, 'urlopen', return_value=self.response(b'12345678-abcd')):
            deploy.refresh_indexnow_key(self.root)
        self.assertEqual(key_path.read_bytes(), b'12345678-abcd')
        with patch.object(deploy.urllib.request, 'urlopen', return_value=self.response(b'<html>failed</html>')):
            with self.assertRaises(ValueError):
                deploy.refresh_indexnow_key(self.root)
        self.assertEqual(key_path.read_bytes(), b'12345678-abcd')
        with patch.object(deploy.urllib.request, 'urlopen', side_effect=deploy.urllib.error.HTTPError('',404,'',None,None)):
            deploy.refresh_indexnow_key(self.root)
        self.assertFalse(key_path.exists())


if __name__ == "__main__":
    unittest.main()
