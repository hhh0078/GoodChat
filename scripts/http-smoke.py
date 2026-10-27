"""文件作用说明：使用本机私有验收账号检查真实容器HTTP接口、RLS授权入口和CSV下载，不发送业务邮件或创建业务记录。"""
import csv
import io
import json
from http.cookiejar import CookieJar
from pathlib import Path
from urllib.error import HTTPError
from urllib.request import HTTPCookieProcessor, Request, build_opener

ROOT = Path(__file__).resolve().parents[1]
BASE = "http://127.0.0.1:13000"


def request(opener, path, body=None, tenant_id=None):
    """调用固定本机应用并返回状态、响应头和正文，不打印登录数据。"""
    headers = {"Origin": BASE}
    if body is not None:
        headers["Content-Type"] = "application/json"
    if tenant_id:
        headers["X-Tenant-ID"] = tenant_id
    query = Request(BASE + path, data=None if body is None else json.dumps(body).encode(), headers=headers)
    try:
        with opener.open(query, timeout=35) as response:
            return response.status, response.headers, response.read()
    except HTTPError as error:
        return error.code, error.headers, error.read()


def main():
    """检查健康、报价、权限、统计、装修资源与正文导出，并保存不含凭证的证据。"""
    account = json.loads((ROOT / ".local/bootstrap-account.json").read_text())
    opener = build_opener(HTTPCookieProcessor(CookieJar()))
    checks = []
    status, _, body = request(opener, "/api/v1/auth/me")
    assert status == 401, "未登录身份接口应拒绝"
    checks.append({"path": "/api/v1/auth/me", "without_login": 401})
    status, _, body = request(opener, "/api/v1/auth/login", {"email": account["email"], "password": account["password"]})
    assert status == 200, "验收账号登录失败"
    assert json.loads(body)["data"]["user"]["is_platform_admin"], "平台初始化身份缺失"
    tenant = account["tenant_id"]
    for path in ["/healthz", "/api/v1/public/plans", "/api/v1/tenant/subscription", "/api/v1/tenant/statistics", "/api/v1/tenant/callback-settings", "/api/v1/tenant/callback-deliveries", "/api/v1/tenant/access-links", "/api/v1/tenant/contacts", "/api/v1/tenant/chat-records", "/api/v1/tenant/templates", "/api/v1/tenant/pages", "/api/v1/tenant/ai-configuration", "/api/v1/platform/tenants", "/api/v1/platform/plans", "/api/v1/platform/templates"]:
        status, _, body = request(opener, path, tenant_id=tenant)
        assert status == 200, f"接口失败：{path} HTTP{status}"
        data = json.loads(body)["data"]
        assert data is not None, f"缺少data信封：{path}"
        if path == "/healthz":
            assert data["status"] == "ok" and data["database"] and data["redis"]
        if path == "/api/v1/public/plans":
            free = next(plan for plan in data["items"] if plan["code"] == "free")
            assert free["price_amount"] == 0 and free["seat_limit"] == 2 and free["validity_days"] == 0
        checks.append({"path": path, "status": status})
    for messages in [False, True]:
        path = "/api/v1/tenant/chat-records/export?include_messages=" + str(messages).lower()
        status, headers, body = request(opener, path, tenant_id=tenant)
        assert status == 200 and headers.get("Content-Type", "").startswith("text/csv"), "导出未返回CSV"
        rows = list(csv.reader(io.StringIO(body.decode("utf-8-sig"))))
        assert rows and rows[0][0] == "会话编号", "导出表头错误"
        assert len(rows) - 1 == int(headers["X-Export-Row-Count"]), "导出行数不一致"
        assert ("消息正文" in rows[0]) == messages, "元数据与正文导出混淆"
        checks.append({"path": path, "status": status, "rows": len(rows) - 1})
    status, _, _ = request(opener, "/api/v1/auth/logout", {})
    assert status == 200
    target = ROOT / ".local/evidence/http-smoke.json"
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps({"checks": checks, "passed": len(checks)}, ensure_ascii=False, indent=2))
    print(f"真实容器HTTP验收通过：{len(checks)}项（未发送业务邮件或创建业务记录）")


if __name__ == "__main__":
    main()
