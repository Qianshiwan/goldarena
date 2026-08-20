import urllib.request, urllib.error, json, base64, io, time, sys

try:
    from PIL import Image
except ImportError:
    import subprocess, sys as _sys
    subprocess.check_call([_sys.executable, "-m", "pip", "install", "-q", "Pillow"])
    from PIL import Image

BASE = "http://localhost:8080/api/v1"

def call(method, path, token=None, body=None):
    url = BASE + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            return r.status, json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read().decode())

def solve_captcha():
    st, cap = call("GET", "/auth/captcha")
    assert st == 200, ("captcha get failed " + str(cap))
    key = cap["data"]["key"]
    bg = cap["data"]["image"].split(",", 1)[1]
    img = Image.open(io.BytesIO(base64.b64decode(bg))).convert("L")
    W, H = img.size
    px = list(img.getdata())
    mean = sum(px) / len(px)
    th = mean * 0.75
    xs = [x for i, p in enumerate(px) if p < th for x in (i % W,)]
    # average x of dark pixels => hole center
    target_x = int(sum(xs) / len(xs))
    st, v = call("POST", "/auth/captcha/verify", body={"key": key, "x": target_x, "track": []})
    assert st == 200, ("captcha verify failed x=%d %s" % (target_x, v))
    return v["data"]["ticket"]

def send_reset_code(email):
    ticket = solve_captcha()
    st, r = call("POST", "/auth/send-reset-code", body={"email": email, "captcha_ticket": ticket})
    assert st == 200, ("send-reset-code failed " + str(r))
    return r["data"].get("dev_code")

print("=" * 60)
print("找回账号 / 重置密码 端到端验证")
print("=" * 60)

# 1) 登录管理员拿 token + 找到管理员邮箱
st, login = call("POST", "/auth/login", body={"username": "admin", "password": "Admin@8888"})
assert st == 200, ("admin login failed " + str(login))
token = login["data"]["access_token"]
st, users = call("GET", "/admin/users", token=token)
admin_email = None
for u in users["data"]["list"]:
    if u.get("role") == "admin":
        admin_email = u["email"]
        break
assert admin_email, "找不到管理员邮箱"
print(f"[1] 管理员邮箱 = {admin_email}")

NEW_PW = "Recover@2026"

# 2) 走找回流程: 发码 -> 重置 -> 返回用户名
dev_code = send_reset_code(admin_email)
print(f"[2] 取得重置验证码(dev) = {dev_code}")
st, r = call("POST", "/auth/reset-password", body={
    "email": admin_email, "code": dev_code, "new_password": NEW_PW})
assert st == 200, ("reset-password failed " + str(r))
print(f"[3] 重置成功, 找回的登录账号 username = {r['data']['username']}  昵称 = {r['data']['nickname']}")

# 3) 用新密码登录
st, login2 = call("POST", "/auth/login", body={"username": r["data"]["username"], "password": NEW_PW})
assert st == 200, ("login with new password failed " + str(login2))
print(f"[4] 用新密码登录成功, role = {login2['data']['role']}")

# 4) 还原管理员密码为原始 Admin@8888 (保持用户已知凭据不变)
dev_code2 = send_reset_code(admin_email)
st, r2 = call("POST", "/auth/reset-password", body={
    "email": admin_email, "code": dev_code2, "new_password": "Admin@8888"})
assert st == 200, ("restore password failed " + str(r2))
st, login3 = call("POST", "/auth/login", body={"username": "admin", "password": "Admin@8888"})
assert st == 200, "restore login failed"
print("[5] 管理员密码已还原为 Admin@8888, 登录验证通过")

print("=" * 60)
print("结论: 找回账号/重置密码 全链路可用 (滑块验证->发码->重置->找回用户名->登录)。")
print("=" * 60)
