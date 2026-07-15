#!/usr/bin/env python3
"""Capture junkie screen baselines for the htmx -> Vue migration.

Shoots every screen/state at desktop (1280x900) and mobile (390x844), dark and
light where it matters. Re-run with --out <dir> after porting a screen to
produce the comparison set; diff the two dirs image-by-image.

Usage:
  python3 capture_baseline.py --out baseline    # against http://localhost:8080
  python3 capture_baseline.py --out vue-step3 --base http://localhost:8080
"""
import argparse
import base64
import http.cookiejar
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

CHROME_CANDIDATES = [
    "/usr/bin/google-chrome-stable",
    os.path.expanduser("~/.cache/ms-playwright/chromium-1228/chrome-linux64/chrome"),
]
PORT = 9231
PROFILE = "/tmp/junkie-baseline-profile"
DESKTOP = {"width": 1280, "height": 900, "deviceScaleFactor": 1, "mobile": False}
MOBILE = {"width": 390, "height": 844, "deviceScaleFactor": 2, "mobile": True}


def http_get(url):
    with urllib.request.urlopen(url) as r:
        return json.loads(r.read().decode())


def http_put(url):
    req = urllib.request.Request(url, method="PUT")
    with urllib.request.urlopen(req) as r:
        return json.loads(r.read().decode())


class CDP:
    def __init__(self, ws_url):
        import websocket

        self.ws = websocket.create_connection(ws_url, max_size=None)
        self._id = 0

    def call(self, method, params=None, timeout=30):
        self._id += 1
        mid = self._id
        self.ws.send(json.dumps({"id": mid, "method": method, "params": params or {}}))
        end = time.time() + timeout
        while time.time() < end:
            data = json.loads(self.ws.recv())
            if data.get("id") == mid:
                if "error" in data:
                    raise RuntimeError(data["error"])
                return data.get("result", {})
        raise TimeoutError(method)

    def eval(self, expr):
        return self.call("Runtime.evaluate", {"expression": expr, "returnByValue": True})

    def close(self):
        self.ws.close()


def start_chrome():
    chrome = next((c for c in CHROME_CANDIDATES if os.path.exists(c)), None)
    if not chrome:
        sys.exit("no chrome binary found")
    os.makedirs(PROFILE, exist_ok=True)
    proc = subprocess.Popen(
        [chrome, f"--remote-debugging-port={PORT}", f"--user-data-dir={PROFILE}",
         "--headless=new", "--disable-gpu", "--no-sandbox", "--remote-allow-origins=*",
         "about:blank"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(40):
        try:
            http_get(f"http://127.0.0.1:{PORT}/json/version")
            return proc
        except Exception:
            time.sleep(0.25)
    proc.kill()
    raise RuntimeError("chrome failed to start")


class Session:
    """One capture context: base URL, output dir, optional auth cookie jar."""

    def __init__(self, base, out):
        self.base = base
        self.out = out

    def shot(self, name, path_url, *, viewport=DESKTOP, theme="dark", opener=None,
             local_storage=None, clicks=(), js=None, wait=1.0):
        tab = http_put(f"http://127.0.0.1:{PORT}/json/new?about:blank")
        cdp = CDP(tab["webSocketDebuggerUrl"])
        try:
            cdp.call("Page.enable")
            cdp.call("Runtime.enable")
            cdp.call("Emulation.setDeviceMetricsOverride", viewport)
            if opener is not None:
                cdp.call("Network.enable")
                cookies = [{"name": c.name, "value": c.value, "domain": "localhost",
                            "path": c.path or "/", "httpOnly": True, "secure": False}
                           for c in opener.cookiejar]
                if cookies:
                    cdp.call("Network.setCookies", {"cookies": cookies})
            # seed localStorage from the app origin before the real navigation
            cdp.call("Page.navigate", {"url": self.base + "/healthz"})
            time.sleep(0.4)
            cdp.eval(f"localStorage.setItem('junkie:theme', {json.dumps(theme)})")
            for key, value in (local_storage or {}).items():
                val = value if isinstance(value, str) else json.dumps(value)
                cdp.eval(f"localStorage.setItem({json.dumps(key)}, {json.dumps(val)})")
            cdp.call("Page.navigate", {"url": self.base + path_url})
            time.sleep(wait)
            for sel in clicks:
                cdp.eval(f"document.querySelector({json.dumps(sel)})?.click()")
                time.sleep(0.35)
            if js:
                cdp.eval(js)
                time.sleep(0.35)
            result = cdp.call("Page.captureScreenshot", {"format": "png", "fromSurface": True})
            path = os.path.join(self.out, name + ".png")
            with open(path, "wb") as f:
                f.write(base64.b64decode(result["data"]))
            print("wrote", path)
        finally:
            cdp.close()


def make_opener():
    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    opener.cookiejar = jar
    return opener


def post_form(opener, base, path, fields):
    data = urllib.parse.urlencode(fields).encode()
    req = urllib.request.Request(base + path, data=data, method="POST")
    try:
        resp = opener.open(req)
        return resp.geturl()
    except urllib.error.HTTPError as e:
        if e.code in (302, 303):
            return e.headers.get("Location", "")
        raise


def signup(base, username, password):
    opener = make_opener()
    post_form(opener, base, "/signup", {"username": username, "password": password, "next": "/dashboard"})
    return opener


GUEST_TODOS = json.dumps([
    {"id": "1", "text": "Review design audit notes", "done": False, "removed": False},
    {"id": "2", "text": "Ship profile heatmap polish", "done": True, "removed": False},
    {"id": "3", "text": "Draft the study plan", "done": True, "removed": True},
])


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True, help="output dir name (created under this script's dir)")
    ap.add_argument("--base", default="http://localhost:8080")
    args = ap.parse_args()
    out = os.path.join(os.path.dirname(os.path.abspath(__file__)), args.out)
    os.makedirs(out, exist_ok=True)

    proc = start_chrome()
    s = Session(args.base, out)
    try:
        run_id = uuid.uuid4().hex[:6]

        # ---- guest, both viewports and themes ----
        for vp, vpn in ((DESKTOP, "desktop"), (MOBILE, "mobile")):
            s.shot(f"guest-desk-dark-{vpn}", "/", viewport=vp)
            s.shot(f"guest-desk-light-{vpn}", "/", viewport=vp, theme="light")
            s.shot(f"guest-desk-todos-{vpn}", "/", viewport=vp,
                   local_storage={"junkie:todos": GUEST_TODOS})
            s.shot(f"login-{vpn}", "/login", viewport=vp)
            s.shot(f"signup-{vpn}", "/login?mode=signup", viewport=vp)
        s.shot("guest-menu", "/", clicks=[".menu-drawer-trigger"])
        s.shot("guest-profile", "/profile")
        ends = time.strftime("%Y-%m-%dT%H:%M:%S.000Z", time.gmtime(time.time() + 45 * 60))
        s.shot("guest-solo-focus", "/",
               local_storage={"junkie:soloTimer": json.dumps({"focusMinutes": 45, "endsAt": ends})})
        s.shot("privacy", "/privacy")
        s.shot("terms", "/terms")
        s.shot("login-error", "/login", js="""
            document.querySelector('input[name=username]').value='nosuchuser';
            document.querySelector('input[name=password]').value='wrongpassword';
            document.querySelector('form').submit();""", wait=1.2)

        # ---- authed flows ----
        u1name = f"audit1{run_id}"
        user1 = signup(args.base, u1name, "auditpass123")
        user2 = signup(args.base, f"audit2{run_id}", "auditpass123")

        for vp, vpn in ((DESKTOP, "desktop"), (MOBILE, "mobile")):
            s.shot(f"desk-loggedin-{vpn}", "/dashboard", viewport=vp, opener=user1)
        s.shot("desk-loggedin-light", "/dashboard", opener=user1, theme="light")
        s.shot("desk-loggedin-menu", "/dashboard", opener=user1, clicks=[".menu-drawer-trigger"])
        s.shot("profile-loggedin", "/profile", opener=user1)
        s.shot("connections-empty", "/connections", opener=user1)
        s.shot("public-profile-self", f"/{u1name}", opener=user1)

        loc = post_form(user1, args.base, "/rooms", {"name": "Design Review Room"})
        room_code = loc.split("/r/")[-1].split("?")[0] if "/r/" in loc else ""
        room = f"/r/{room_code}"
        post_form(user1, args.base, f"{room}/todos", {"text": "Finalize room invite copy"})
        for vp, vpn in ((DESKTOP, "desktop"), (MOBILE, "mobile")):
            s.shot(f"room-todos-{vpn}", room, viewport=vp, opener=user1)
        post_form(user1, args.base, f"{room}/timer-start", {})
        s.shot("room-focus", room, opener=user1, wait=1.4)
        s.shot("room-focus-mobile", room, viewport=MOBILE, opener=user1, wait=1.4)

        post_form(user1, args.base, "/solo/start", {"focus_minutes": "50"})
        s.shot("desk-solo-focus", "/dashboard", opener=user1, wait=1.4)
        post_form(user1, args.base, "/solo/cancel", {})

        s.shot("desk-room-todos", f"/dashboard?todos=room&room={room_code}", opener=user1)
        s.shot("room-as-visitor", room, opener=user2)
        s.shot("join-confirm", f"/join/confirm?code={room_code}", opener=user2)
        s.shot("login-with-next", f"/login?next={urllib.parse.quote('/join/confirm?code=' + room_code)}")
        s.shot("room-not-found", "/r/ZZZZ-ZZZZ", opener=user1)
        s.shot("admin-forbidden", "/admin", opener=user1)

        # ---- admin (flip role directly in the local dev DB) ----
        flip = subprocess.run(
            ["docker", "compose", "exec", "-T", "postgres", "psql", "-U", "junkie", "-d", "junkie",
             "-c", f"UPDATE users SET role='owner' WHERE username='{u1name}';"],
            cwd=os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
            capture_output=True, text=True)
        if flip.returncode == 0:
            s.shot("admin", "/admin", opener=user1, wait=1.2)
        else:
            print("skip admin shot:", flip.stderr.strip()[:120])

        print("done; room_code", room_code)
    finally:
        proc.terminate()
        proc.wait(timeout=5)


if __name__ == "__main__":
    main()
