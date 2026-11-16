"""文件作用说明：验证部署清理只删除GoodChat构建产物，不删除有容器引用的镜像或其他项目资源。"""
import importlib.util
from pathlib import Path
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


if __name__ == "__main__":
    unittest.main()
