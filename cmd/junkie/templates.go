package main

import (
	"fmt"
	"html/template"
	"strings"
	"time"
)

func parseTemplates() *template.Template {
	funcs := template.FuncMap{
		"secondsUntil": func(t time.Time) int {
			seconds := int(time.Until(t).Seconds())
			if seconds < 0 {
				return 0
			}
			return seconds
		},
		"mul":  func(a, b int) int { return a * b },
		"sub":  func(a, b int) int { return a - b },
		"join": strings.Join,
		"rfc3339Nano": func(t time.Time) string {
			return t.UTC().Format(time.RFC3339Nano)
		},
		"timerSeconds": func(t *timerRun) int {
			if t == nil {
				return 0
			}
			if t.PausedRemainingSeconds != nil {
				return *t.PausedRemainingSeconds
			}
			seconds := int(time.Until(t.PhaseEndsAt).Seconds())
			if seconds < 0 {
				return 0
			}
			return seconds
		},
		"focusHours": func(minutes int) int {
			return (minutes + 30) / 60
		},
		"initial": func(name string) string {
			for _, r := range name {
				return strings.ToUpper(string(r))
			}
			return "?"
		},
		"avatarOverflow": func(total, max int) int {
			if total <= max {
				return 0
			}
			return total - max
		},
		"dict": func(vals ...interface{}) (map[string]interface{}, error) {
			if len(vals)%2 != 0 {
				return nil, fmt.Errorf("dict: odd number of arguments")
			}
			m := make(map[string]interface{}, len(vals)/2)
			for i := 0; i < len(vals); i += 2 {
				key, ok := vals[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict: keys must be strings")
				}
				m[key] = vals[i+1]
			}
			return m, nil
		},
		"soloBreakPending": soloBreakPending,
	}
	return template.Must(template.New("junkie").Funcs(funcs).Parse(layoutTemplates))
}

const layoutTemplates = `
{{define "shell"}}
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}} · junkie</title>
  <script>
    (function () {
      var k = 'junkie:theme';
      var t = localStorage.getItem(k);
      if (t !== 'light' && t !== 'dark') {
        t = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
      }
      document.documentElement.setAttribute('data-theme', t);
    })();
  </script>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&family=Source+Serif+4:ital,wght@0,600;1,500&display=swap" rel="stylesheet">
  <link rel="stylesheet" href="/assets/app.css">
  <link rel="icon" href="/assets/icon.svg" type="image/svg+xml">
  <script src="/assets/htmx.min.js"></script>
  <script src="/assets/notifications.js"></script>
</head>
<body{{if or .SoloTimer .FocusMode}} class="focus-active"{{end}}{{if .User.ID}} data-user-id="{{.User.ID}}"{{end}}>
  {{$menu := or (eq .Title "Dashboard") (eq .Title "Profile") (and (ne .Room.Code "") (ne .Title "Join room"))}}
  <header class="topbar">
    <a class="brand" href="/" aria-label="junkie home">
      <span class="brand-mark">j</span>
      <span>junkie</span>
    </a>
    <div class="topbar-actions">
      <button type="button" class="theme-toggle" id="theme-toggle" aria-label="Toggle theme" title="Switch to dark mode">
        <span class="theme-icon theme-icon-dark" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg></span>
        <span class="theme-icon theme-icon-light" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"/></svg></span>
      </button>
      {{if and $menu .User.ID}}
        <span class="topbar-user mono">{{.User.DisplayName}}</span>
      {{end}}
      {{if $menu}}
        <button type="button" class="menu-drawer-trigger" aria-label="Open menu" aria-expanded="false">
          <span class="menu-bar" aria-hidden="true"></span>
          <span class="menu-bar" aria-hidden="true"></span>
          <span class="menu-bar" aria-hidden="true"></span>
        </button>
      {{end}}
    </div>
  </header>
  <main class="page">
    {{if and .Error (ne .Title "Sign in") (ne .Title "Sign up") (ne .Title "Dashboard")}}<p class="notice notice-error">{{.Error}}</p>{{end}}
    {{template "content" .}}
  </main>
  {{if $menu}}
    {{if .GuestMode}}
      {{template "menu-drawer-guest" .}}
    {{else}}
      {{template "menu-drawer-user" .}}
    {{end}}
  {{end}}
  <script>
    (function () {
      const CIRC = 2 * Math.PI * 88;
      const clampMinutes = (n) => Math.min(180, Math.max(5, n));

      const setRing = (timer, ratio) => {
        const ring = timer?.querySelector('.circle-timer-progress');
        if (!ring) return;
        const r = Math.max(0, Math.min(1, ratio));
        ring.style.strokeDashoffset = String(CIRC * (1 - r));
      };

      const wireIdleTimer = (form) => {
        if (!form || form.dataset.timerWired === 'true') return;
        const timer = form.querySelector('.circle-timer.idle');
        const input = form.querySelector('input[name="focus_minutes"]');
        if (!timer || !input) return;
        form.dataset.timerWired = 'true';
        timer.tabIndex = 0;

        const syncDigits = () => {
          const label = input.closest('.circle-timer-time');
          label?.classList.toggle('digits-3', String(input.value).length >= 3);
        };

        const syncRing = () => {
          const minutes = clampMinutes(Number(input.value) || 50);
          input.value = minutes;
          syncDigits();
          setRing(timer, minutes / 180);
        };

        const isReadOnly = () => input.readOnly;
        const adjustMinutes = (delta) => {
          if (isReadOnly()) return;
          input.value = clampMinutes((Number(input.value) || 50) + delta);
          syncRing();
        };

        timer.querySelectorAll('.circle-timer-step').forEach((btn) => {
          btn.addEventListener('click', (event) => {
            event.preventDefault();
            event.stopPropagation();
            adjustMinutes(Number(btn.dataset.delta));
          });
        });

        input.addEventListener('click', (event) => event.stopPropagation());
        input.addEventListener('input', syncRing);
        input.addEventListener('change', syncRing);
        input.addEventListener('keydown', (event) => {
          if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
            if (isReadOnly()) return;
            event.preventDefault();
            event.stopPropagation();
            adjustMinutes(event.key === 'ArrowUp' ? 1 : -1);
            return;
          }
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            event.stopPropagation();
            form.requestSubmit();
          }
        });

        timer.addEventListener('keydown', (event) => {
          if (event.target !== timer) return;
          if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
            if (isReadOnly()) return;
            event.preventDefault();
            adjustMinutes(event.key === 'ArrowUp' ? 1 : -1);
            return;
          }
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            form.requestSubmit();
          }
        });

        let lastWheelAt = 0;
        timer.addEventListener('wheel', (event) => {
          if (isReadOnly() || event.deltaY === 0) return;
          event.preventDefault();
          const now = performance.now();
          if (now - lastWheelAt < 80) return;
          lastWheelAt = now;
          adjustMinutes(event.deltaY < 0 ? 1 : -1);
        }, { passive: false });

        timer.addEventListener('click', () => form.requestSubmit());
        syncRing();
      };

      document.querySelectorAll('.circle-timer-form').forEach(wireIdleTimer);

      const timerPhase = (box) => {
        const card = box.closest('.timer-card');
        if (card?.classList.contains('lobby')) return 'lobby';
        if (card?.classList.contains('break')) return 'break';
        if (card?.classList.contains('focus')) return 'focus';
        if (box.closest('.circle-timer.break-running')) return 'break';
        if (box.closest('.circle-timer.running')) return 'focus';
        return 'focus';
      };

      document.querySelectorAll('[data-seconds]').forEach((box) => {
        let left = Number(box.dataset.seconds || 0);
        const total = Number(box.dataset.total || left) || 1;
        const paused = box.dataset.paused === 'true';
        const timer = box.closest('.circle-timer');
        const phase = timerPhase(box);
        const soloTimer = box.closest('.solo-timer');
        // Count down against an absolute deadline instead of decrementing
        // once per tick: background tabs get their intervals throttled or
        // suspended, so lost ticks left the display behind real time until
        // a manual refresh.
        const deadline = Date.now() + left * 1000;
        let notified = false;
        const paint = () => {
          if (!paused) left = Math.max(0, Math.ceil((deadline - Date.now()) / 1000));
          const m = String(Math.floor(left / 60)).padStart(2, '0');
          const s = String(left % 60).padStart(2, '0');
          box.textContent = m + ':' + s;
          const live = box.closest('[aria-live]') || box.parentElement;
          if (live && left % 60 === 0) live.setAttribute('aria-label', m + ' minutes remaining');
          setRing(timer, left / total);
          if (left <= 0 && !notified && !paused) {
            notified = true;
            const deskView = box.closest('.desk-timer-view');
            if (!deskView || !deskView.hidden) {
              if (phase !== 'lobby') window.junkieNotify?.onTimerEnd(phase);
              const roomSync = box.closest('[data-room-sync]');
              setTimeout(() => {
                if (roomSync && window.junkieReconcileRoomTimer) {
                  window.junkieReconcileRoomTimer('countdown');
                } else {
                  window.junkieStashTypedTodo?.();
                  location.reload();
                }
              }, 400);
            }
          }
        };
        paint();
        setInterval(paint, 1000);
        document.addEventListener('visibilitychange', () => {
          if (document.visibilityState === 'visible') paint();
        });
      });

      document.querySelectorAll('form[action="/solo/start"], form[action$="/timer-start"]').forEach((form) => {
        form.addEventListener('submit', (event) => {
          window.junkieNotify?.requestPermission();
          const roomName = form.dataset.roomName;
          if (roomName && !confirm('You\'re about to start a focus block for ' + roomName + '.')) {
            event.preventDefault();
          }
        });
      });

      document.querySelectorAll('form[action="/rooms"], form[action="/rooms/join"], form[action="/join/confirm"]').forEach((form) => {
        form.addEventListener('submit', () => window.junkieNotify?.requestPermission());
      });

      const roomInviteToggle = document.getElementById('room-invite-notifications');
      if (roomInviteToggle) {
        roomInviteToggle.checked = window.junkieNotify?.roomInvitesEnabled() !== false;
        roomInviteToggle.addEventListener('change', () => {
          window.junkieNotify?.setRoomInvitesEnabled(roomInviteToggle.checked);
        });
      }

      window.junkieCircleTimer = { CIRC, clampMinutes, setRing, wireIdleTimer };

      const todoFocusKey = 'junkie:todoFocus';
      if (sessionStorage.getItem(todoFocusKey)) {
        sessionStorage.removeItem(todoFocusKey);
        document.querySelector('form.inline-form input[name="text"]')?.focus();
      }

      document.querySelectorAll('form.inline-form').forEach((form) => {
        const input = form.querySelector('input[name="text"]');
        if (!input) return;
        const action = form.getAttribute('action') || '';
        if (!action.endsWith('/todos')) return;
        form.addEventListener('submit', () => {
          sessionStorage.setItem(todoFocusKey, '1');
        });
      });

      const wireMenuDrawer = () => {
        const trigger = document.querySelector('.menu-drawer-trigger');
        const drawer = document.querySelector('.menu-drawer');
        const backdrop = document.querySelector('.menu-drawer-backdrop');
        const closeBtn = document.querySelector('.menu-drawer-close');
        if (!trigger || !drawer) return;

        const open = () => {
          drawer.classList.add('open');
          backdrop?.removeAttribute('hidden');
          drawer.setAttribute('aria-hidden', 'false');
          trigger.setAttribute('aria-expanded', 'true');
          document.body.classList.add('menu-drawer-open');
          drawer.querySelector('a, button, input, summary')?.focus();
        };
        const shut = () => {
          drawer.classList.remove('open');
          backdrop?.setAttribute('hidden', '');
          drawer.setAttribute('aria-hidden', 'true');
          trigger.setAttribute('aria-expanded', 'false');
          document.body.classList.remove('menu-drawer-open');
          trigger.focus();
        };

        trigger.addEventListener('click', open);
        closeBtn?.addEventListener('click', shut);
        backdrop?.addEventListener('click', shut);
        document.addEventListener('keydown', (event) => {
          if (event.key === 'Escape') shut();
        });
      };
      wireMenuDrawer();

      const themeKey = 'junkie:theme';
      const themeToggle = document.getElementById('theme-toggle');
      const syncThemeToggle = () => {
        const dark = document.documentElement.getAttribute('data-theme') === 'dark';
        const label = dark ? 'Switch to light mode' : 'Switch to dark mode';
        themeToggle?.setAttribute('aria-label', label);
        themeToggle?.setAttribute('title', label);
      };
      themeToggle?.addEventListener('click', () => {
        const next = document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
        document.documentElement.setAttribute('data-theme', next);
        localStorage.setItem(themeKey, next);
        syncThemeToggle();
      });
      syncThemeToggle();

      const wirePasswordToggles = () => {
        document.querySelectorAll('.password-toggle').forEach((toggle) => {
          if (toggle.dataset.wired === 'true') return;
          toggle.dataset.wired = 'true';
          const input = toggle.closest('.password-field')?.querySelector('input');
          if (!input) return;
          toggle.addEventListener('click', () => {
            const visible = input.type === 'password';
            input.type = visible ? 'text' : 'password';
            toggle.classList.toggle('is-visible', visible);
            const label = visible ? 'Hide password' : 'Show password';
            toggle.setAttribute('aria-label', label);
            toggle.setAttribute('aria-pressed', visible ? 'true' : 'false');
          });
        });
      };
      wirePasswordToggles();

      const wireFocusTodosPeek = () => {
        if (document.getElementById('guest-desk')) return;
        const toggle = document.querySelector('.focus-todos-toggle');
        const panel = document.querySelector('.focus-todos-panel');
        const backdrop = document.querySelector('.focus-todos-backdrop');
        const pinBtn = document.querySelector('.focus-todos-pin');
        if (!toggle || !panel) return;

        const pinKey = 'junkie:todosPinned';
        const peekKey = 'junkie:peekSeen';
        let pinned = localStorage.getItem(pinKey) === '1';
        let hideTimer = null;
        let open = false;

        const syncOpen = (next) => {
          open = next;
          panel.classList.toggle('is-open', open);
          toggle.classList.toggle('is-open', open);
          toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
          panel.setAttribute('aria-hidden', open ? 'false' : 'true');
          if (open && !pinned) backdrop?.removeAttribute('hidden');
          else backdrop?.setAttribute('hidden', '');
        };

        const close = () => {
          if (hideTimer) {
            clearTimeout(hideTimer);
            hideTimer = null;
          }
          syncOpen(false);
        };

        const dismiss = () => {
          if (pinned) return;
          close();
        };

        const scheduleDismiss = () => {
          if (pinned) return;
          if (hideTimer) clearTimeout(hideTimer);
          hideTimer = setTimeout(dismiss, 10000);
        };

        const show = () => {
          syncOpen(true);
          scheduleDismiss();
        };

        if (pinBtn) {
          pinBtn.classList.toggle('is-pinned', pinned);
          pinBtn.addEventListener('click', (event) => {
            event.stopPropagation();
            pinned = !pinned;
            localStorage.setItem(pinKey, pinned ? '1' : '0');
            pinBtn.classList.toggle('is-pinned', pinned);
            if (pinned) {
              if (hideTimer) {
                clearTimeout(hideTimer);
                hideTimer = null;
              }
              backdrop?.setAttribute('hidden', '');
            } else if (open) {
              backdrop?.removeAttribute('hidden');
              scheduleDismiss();
            }
          });
        }
        if (!localStorage.getItem(peekKey)) {
          toggle.classList.add('peek-pulse');
          localStorage.setItem(peekKey, '1');
          setTimeout(() => toggle.classList.remove('peek-pulse'), 2400);
        }

        toggle.addEventListener('click', () => {
          if (open) close();
          else show();
        });
        backdrop?.addEventListener('click', dismiss);
      };
      wireFocusTodosPeek();

      const wireDeskTodosSwitcher = () => {
        const panel = document.querySelector('.desk-todos-panel[data-has-rooms="true"]');
        if (!panel) return;

        const modeKey = 'junkie:deskTodosMode';
        const roomKey = 'junkie:deskTodosRoom';
        const switcher = panel.querySelector('.desk-todos-switch');
        const trigger = panel.querySelector('.desk-todos-mode');
        const menu = panel.querySelector('.desk-todos-menu');
        const label = panel.querySelector('.desk-todos-mode-label');
        const hint = panel.querySelector('.desk-todos-hint');
        const views = panel.querySelectorAll('.desk-todos-view');
        const options = menu ? Array.from(menu.querySelectorAll('[data-mode]')) : [];
        const membershipPill = document.querySelector('.room-membership-pill');
        const membershipBar = membershipPill?.closest('.desk-room-bar');
        const membershipName = membershipPill?.querySelector('.room-membership-name');
        const roomCodes = options.filter((opt) => opt.dataset.mode === 'room').map((opt) => opt.dataset.room);
        const defaultRoom = roomCodes[0] || '';
        const params = new URLSearchParams(location.search);

        let mode = 'room';
        let room = params.get('room') || sessionStorage.getItem(roomKey) || defaultRoom;
        if (params.get('todos') === 'private') {
          mode = 'private';
        } else if (params.get('todos') === 'room') {
          mode = 'room';
        } else {
          const savedMode = sessionStorage.getItem(modeKey);
          if (savedMode === 'private') mode = 'private';
          else if (savedMode === 'room' && roomCodes.length) mode = 'room';
          else if (roomCodes.length) mode = 'room';
          else mode = 'private';
        }
        if (mode === 'room' && !roomCodes.includes(room)) room = defaultRoom;

        const shut = () => {
          menu?.setAttribute('hidden', '');
          trigger?.setAttribute('aria-expanded', 'false');
        };

        const apply = () => {
          let activeRoom = null;
          if (mode === 'private') {
            label.textContent = 'Private';
            hint.textContent = '';
            hint?.setAttribute('hidden', '');
            hint?.classList.remove('label-warn', 'label-accent');
            membershipBar?.setAttribute('hidden', '');
            views.forEach((view) => {
              view.hidden = view.dataset.mode !== 'private';
            });
            options.forEach((opt) => {
              opt.classList.toggle('is-active', opt.dataset.mode === 'private');
            });
          } else {
            activeRoom = options.find((opt) => opt.dataset.mode === 'room' && opt.dataset.room === room);
            label.textContent = activeRoom?.dataset.roomName || activeRoom?.textContent.trim() || 'Room todos';
            hint.textContent = 'Public to the room';
            hint?.removeAttribute('hidden');
            hint?.classList.add('label-warn');
            hint?.classList.remove('label-accent');
            membershipBar?.removeAttribute('hidden');
            views.forEach((view) => {
              view.hidden = !(view.dataset.mode === 'room' && view.dataset.room === room);
            });
            options.forEach((opt) => {
              opt.classList.toggle('is-active', opt.dataset.mode === 'room' && opt.dataset.room === room);
            });
          }

          const contextRoom = mode === 'room' ? room : '';
          const contextName = mode === 'room'
            ? (activeRoom?.dataset.roomName || activeRoom?.textContent.trim() || room)
            : '';
          if (membershipPill && contextRoom) {
            membershipPill.href = '/r/' + encodeURIComponent(contextRoom);
            if (membershipName && contextName) membershipName.textContent = contextName;
          }

          sessionStorage.setItem(modeKey, mode);
          if (mode === 'room') sessionStorage.setItem(roomKey, room);
          const nextURL = new URL(location.href);
          nextURL.searchParams.set('todos', mode);
          if (mode === 'room') nextURL.searchParams.set('room', room);
          else nextURL.searchParams.delete('room');
          history.replaceState(null, '', nextURL);

          panel.dispatchEvent(new CustomEvent('desk-todos-mode-change', {
            detail: {
              mode,
              room: mode === 'room' ? room : '',
              roomName: mode === 'room' ? contextName : '',
            },
          }));
        };

        trigger?.addEventListener('click', () => {
          const open = menu?.hasAttribute('hidden');
          if (open) {
            menu?.removeAttribute('hidden');
            trigger.setAttribute('aria-expanded', 'true');
          } else {
            shut();
          }
        });

        options.forEach((opt) => {
          opt.addEventListener('click', () => {
            mode = opt.dataset.mode || 'private';
            if (opt.dataset.room) room = opt.dataset.room;
            apply();
            shut();
          });
        });

        document.addEventListener('click', (event) => {
          if (!switcher?.contains(event.target)) shut();
        });
        document.addEventListener('keydown', (event) => {
          if (event.key === 'Escape') shut();
        });

        apply();
      };
      wireDeskTodosSwitcher();

      const wireDeskTimerMode = () => {
        const panel = document.querySelector('.desk-todos-panel[data-has-rooms="true"]');
        const column = document.querySelector('.desk-ring-column');
        if (!panel || !column) return;

        const views = Array.from(column.querySelectorAll('.desk-timer-view'));
        const modeKey = 'junkie:deskTodosMode';
        const roomKey = 'junkie:deskTodosRoom';

        const sync = (event) => {
          const context = event?.detail;
          const mode = context?.mode || sessionStorage.getItem(modeKey) || 'room';
          const room = context?.room || sessionStorage.getItem(roomKey) || panel.querySelector('.desk-todos-view[data-mode="room"]')?.dataset.room || '';
          views.forEach((view) => {
            view.hidden = mode === 'private'
              ? view.dataset.mode !== 'private'
              : !(view.dataset.mode === 'room' && view.dataset.room === room);
          });
        };

        panel.addEventListener('desk-todos-mode-change', sync);
        sync();
      };
      wireDeskTimerMode();

      const roomStatusFromElement = (el) => ({
        runId: el?.dataset.runId || '',
        phase: el?.dataset.phase || 'idle',
        endsAt: el?.dataset.endsAt || '',
        currentSession: Number(el?.dataset.currentSession || 0),
        totalSessions: Number(el?.dataset.totalSessions || 0),
        participant: el?.dataset.participant === 'true',
        participantCount: Number(el?.dataset.participantCount || 0),
        paused: el?.dataset.paused === 'true',
        pausedRemainingSeconds: Number(el?.dataset.pausedRemaining || 0),
      });

      const selectedRoomTimer = () => {
        const roomPage = document.querySelector('[data-room-sync][data-room-page]');
        if (roomPage) return roomPage;
        return document.querySelector('.desk-timer-view[data-room-sync]:not([hidden])');
      };

      const sameRoomStatus = (current, server) => {
        const currentEnd = current.endsAt ? Date.parse(current.endsAt) : 0;
        const serverEnd = server.endsAt ? Date.parse(server.endsAt) : 0;
        const paused = Boolean(server.paused);
        const pausedRemainingMatch = !paused ||
          current.pausedRemainingSeconds === Number(server.pausedRemainingSeconds || 0);
        return current.runId === (server.runId || '') &&
          current.phase === (server.phase || 'idle') &&
          currentEnd === serverEnd &&
          current.currentSession === Number(server.currentSession || 0) &&
          current.totalSessions === Number(server.totalSessions || 0) &&
          current.participant === Boolean(server.participant) &&
          current.participantCount === Number(server.participantCount || 0) &&
          current.paused === paused &&
          pausedRemainingMatch;
      };

      let roomStatusRequest = null;
      let roomStatusReloading = false;
      let lastRoomStatusCheck = 0;
      const reconcileRoomTimer = async (reason) => {
        if (document.visibilityState === 'hidden' || roomStatusReloading) return;
        const el = selectedRoomTimer();
        const code = el?.dataset.roomSync;
        if (!code) return;
        const now = Date.now();
        if (roomStatusRequest || (reason !== 'countdown' && now - lastRoomStatusCheck < 1000)) {
          return roomStatusRequest;
        }
        lastRoomStatusCheck = now;
        roomStatusRequest = fetch('/r/' + encodeURIComponent(code) + '/timer-status', {
          credentials: 'same-origin',
          headers: { Accept: 'application/json' },
          cache: 'no-store',
        }).then(async (response) => {
          if (!response.ok) return;
          const server = await response.json();
          if (!sameRoomStatus(roomStatusFromElement(el), server)) {
            roomStatusReloading = true;
            window.junkieStashTypedTodo?.();
            location.reload();
          }
        }).catch(() => {
          // A later focus, visibility, online, or periodic check retries.
        }).finally(() => {
          roomStatusRequest = null;
        });
        return roomStatusRequest;
      };
      window.junkieReconcileRoomTimer = reconcileRoomTimer;

      document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible') reconcileRoomTimer('visible');
      });
      window.addEventListener('focus', () => reconcileRoomTimer('focus'));
      window.addEventListener('online', () => reconcileRoomTimer('online'));
      setInterval(() => {
        if (document.visibilityState === 'visible') reconcileRoomTimer('periodic');
      }, 45000);

      const escapeHTML = (value) => String(value || '').replace(/[&<>"']/g, (char) => ({
        '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
      }[char]));

      const showFocusJoinPrompt = (code, roomName, starterName, lobbyDeadline) => {
        if (document.getElementById('focus-join-prompt')) return;
        if (document.querySelector('.room-focus-shell')) return;

        const deadline = Date.parse(lobbyDeadline || '') || Date.now() + 10000;
        const backdrop = document.createElement('div');
        backdrop.id = 'focus-join-prompt';
        backdrop.className = 'join-prompt-backdrop';
        backdrop.innerHTML =
          '<div class="join-prompt-card panel" role="dialog" aria-labelledby="join-prompt-title">' +
            '<p class="label label-accent">Starting · join now</p>' +
            '<h2 id="join-prompt-title">' + escapeHTML(starterName) + ' is starting a focus block in ' + escapeHTML(roomName) + '. Join?</h2>' +
            '<p class="join-prompt-countdown mono" aria-live="polite">00:10</p>' +
            '<div class="join-prompt-actions">' +
              '<form method="post" action="/r/' + encodeURIComponent(code) + '/timer-join">' +
                '<button type="submit" class="btn-primary">Join</button>' +
              '</form>' +
              '<button type="button" class="btn-ghost" data-dismiss>Not now</button>' +
            '</div>' +
          '</div>';
        const dismiss = () => {
          backdrop.remove();
          const onRoomPage = document.querySelector('[data-room-page]')?.dataset.roomSync === code;
          if (onRoomPage || deskTodosViewingRoom(code)) {
            window.junkieStashTypedTodo?.();
            location.reload();
          }
        };
        backdrop.querySelector('[data-dismiss]')?.addEventListener('click', dismiss);
        backdrop.addEventListener('click', (event) => {
          if (event.target === backdrop) dismiss();
        });
        document.body.appendChild(backdrop);
        backdrop.querySelector('.btn-primary')?.focus();

        const countdown = backdrop.querySelector('.join-prompt-countdown');
        const paint = () => {
          const left = Math.max(0, Math.ceil((deadline - Date.now()) / 1000));
          countdown.textContent = '00:' + String(left).padStart(2, '0');
          if (left === 0) {
            clearInterval(interval);
            backdrop.remove();
            window.junkieStashTypedTodo?.();
            location.reload();
          }
        };
        const interval = setInterval(paint, 250);
        paint();
        window.junkieNotify?.onRoomInvite(starterName, roomName, () => {
          backdrop.querySelector('.btn-primary')?.focus();
        });
      };
      window.junkieShowFocusJoinPrompt = showFocusJoinPrompt;

      const deskTodosViewingRoom = (code) => {
        const panel = document.querySelector('.desk-todos-panel[data-has-rooms="true"]');
        if (!panel) return false;
        const mode = sessionStorage.getItem('junkie:deskTodosMode') || 'room';
        if (mode !== 'room') return false;
        const room = sessionStorage.getItem('junkie:deskTodosRoom') || panel.querySelector('.desk-todos-view[data-mode="room"]')?.dataset.room || '';
        return room === code;
      };

      window.junkieOnRoomWSMessage = (code, msg, roomName) => {
        let event = null;
        if (typeof msg === 'string' && msg.startsWith('{')) {
          try { event = JSON.parse(msg); } catch (_) {}
        }
        const type = event?.type || msg;
        if (type === 'timer-lobby' && !document.querySelector('.room-focus-shell')) {
          if (event?.starterUserId === document.body.dataset.userId) {
            if (document.querySelector('[data-room-page]') || deskTodosViewingRoom(code)) {
              setTimeout(() => {
                window.junkieStashTypedTodo?.();
                location.reload();
              }, 100);
            }
            return;
          }
          showFocusJoinPrompt(
            event?.roomCode || code,
            event?.roomName || roomName,
            event?.starterName || 'A room member',
            event?.lobbyDeadline || ''
          );
          return;
        }
        if (type === 'deleted') {
          if (document.querySelector('.room-shell') || document.querySelector('.room-focus-page')) {
            window.location = '/';
          }
          return;
        }
        const onRoomPage = document.querySelector('.room-shell') || document.querySelector('.room-focus-page');
        if (type === 'todos') {
          if (!onRoomPage && !deskTodosViewingRoom(code)) return;
          // hub.broadcast includes the sender's own connection, so this also
          // patches the tab that made the change -- it just re-fetches the
          // same fragment its own htmx swap already applied. Patch instead
          // of location.reload() so completing a task never blows away the
          // rest of the page (typed drafts, open panels, running timers).
          const desk = !onRoomPage;
          const target = document.getElementById('todo-groups-' + code);
          if (!target) return;
          fetch('/r/' + code + '/todos-fragment' + (desk ? '?desk=1' : ''), {
            headers: { 'HX-Request': 'true' },
          })
            .then((resp) => (resp.ok ? resp.text() : null))
            .then((html) => {
              if (html == null) return;
              target.outerHTML = html;
              const fresh = document.getElementById('todo-groups-' + code);
              if (fresh) {
                window.htmx?.process(fresh);
                window.junkieWireTodoGroupCollapse?.();
              }
            })
            .catch(() => {});
          return;
        }
        if (!onRoomPage && !deskTodosViewingRoom(code)) {
          return;
        }
        setTimeout(() => {
          window.junkieStashTypedTodo?.();
          location.reload();
        }, 200);
      };

      const wireDeskRoomWS = () => {
        document.querySelectorAll('[data-room-ws]').forEach((el) => {
          const code = el.dataset.roomWs;
          const name = el.dataset.roomName;
          if (!code) return;
          let ws = null;
          let retryTimer = null;
          let retryCount = 0;
          const connect = () => {
            if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return;
            clearTimeout(retryTimer);
            ws = new WebSocket((location.protocol === 'https:' ? 'wss' : 'ws') + '://' + location.host + '/ws/r/' + encodeURIComponent(code));
            ws.onopen = () => {
              retryCount = 0;
              reconcileRoomTimer('ws-open');
            };
            ws.onmessage = (event) => window.junkieOnRoomWSMessage(code, event.data, name);
            ws.onerror = () => ws?.close();
            ws.onclose = () => {
              ws = null;
              const delay = Math.min(30000, 1000 * (2 ** Math.min(retryCount, 5)));
              retryCount++;
              retryTimer = setTimeout(connect, delay + Math.floor(Math.random() * 500));
            };
          };
          connect();
          window.addEventListener('online', connect);
          window.addEventListener('focus', connect);
          document.addEventListener('visibilitychange', () => {
            if (document.visibilityState === 'visible') connect();
          });
        });
      };
      wireDeskRoomWS();

      // Personal state (private todos, solo timer) syncs across the same
      // user's devices over a per-user channel, mirroring the room socket.
      const wireUserWS = () => {
        if (!document.body.dataset.userId) return;
        let ws = null;
        let retryTimer = null;
        let retryCount = 0;
        let soloReloading = false;

        const patchList = (id, url) => {
          const target = document.getElementById(id);
          if (!target) return;
          fetch(url, { headers: { 'HX-Request': 'true' } })
            .then((resp) => (resp.ok ? resp.text() : null))
            .then((html) => {
              if (html == null) return;
              target.outerHTML = html;
              const fresh = document.getElementById(id);
              if (fresh) window.htmx?.process(fresh);
            })
            .catch(() => {});
        };

        const onMessage = (msg) => {
          if (msg === 'todos') {
            patchList('personal-todos-list', '/todos-fragment');
            patchList('focus-todos-list', '/todos-fragment?view=focus');
            return;
          }
          if (msg === 'solo-timer') {
            // Solo timer changes swap the whole desk between idle/focus/break
            // shells, which (like room timer phases) still works via reload.
            // Only the dashboard shows the solo timer, and the device that
            // made the change already navigated, so this reaches other tabs.
            if (soloReloading) return;
            if (!document.querySelector('.desk-shell, .focus-desk')) return;
            soloReloading = true;
            setTimeout(() => {
              window.junkieStashTypedTodo?.();
              location.reload();
            }, 200);
          }
        };

        const connect = () => {
          if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return;
          clearTimeout(retryTimer);
          ws = new WebSocket((location.protocol === 'https:' ? 'wss' : 'ws') + '://' + location.host + '/ws/me');
          ws.onopen = () => {
            retryCount = 0;
          };
          ws.onmessage = (event) => onMessage(String(event.data));
          ws.onclose = () => {
            ws = null;
            const delay = Math.min(30000, 1000 * (2 ** Math.min(retryCount, 5)));
            retryCount++;
            retryTimer = setTimeout(connect, delay + Math.floor(Math.random() * 500));
          };
        };
        connect();
        window.addEventListener('online', connect);
        window.addEventListener('focus', connect);
        document.addEventListener('visibilitychange', () => {
          if (document.visibilityState === 'visible') connect();
        });
      };
      wireUserWS();

      const wireTodoGroupCollapse = () => {
        document.querySelectorAll('.todo-groups').forEach((groups) => {
          const room = groups.dataset.room || '';
          groups.querySelectorAll('.todo-group').forEach((section) => {
            const group = section.dataset.group;
            if (!group) return;
            const key = 'junkie:todoGroup:' + group + ':' + room;
            const toggle = section.querySelector('.todo-group-toggle');
            if (!toggle) return;

            const setCollapsed = (collapsed) => {
              section.classList.toggle('is-collapsed', collapsed);
              toggle.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
              sessionStorage.setItem(key, collapsed ? 'collapsed' : 'expanded');
            };

            setCollapsed(sessionStorage.getItem(key) === 'collapsed');

            if (toggle.dataset.wired === 'true') return;
            toggle.dataset.wired = 'true';
            toggle.addEventListener('click', () => {
              setCollapsed(!section.classList.contains('is-collapsed'));
            });
          });
        });
      };
      wireTodoGroupCollapse();
      window.junkieWireTodoGroupCollapse = wireTodoGroupCollapse;
      // htmx replaces the whole .todo-groups container on every todo action,
      // so the click listeners above need to be re-attached to the fresh
      // nodes each time (collapsed/expanded state itself lives in
      // sessionStorage, so it survives the swap either way).
      document.body.addEventListener('htmx:afterSwap', (event) => {
        if (event.target.closest?.('.todo-groups') || event.target.classList?.contains('todo-groups')) {
          wireTodoGroupCollapse();
        }
      });

      // The add button doubles as an affordance: gray while the input is
      // empty, accent-colored once there is text to submit.
      const syncTodoAddButton = (form) => {
        const input = form.querySelector('input[name="text"]');
        const btn = form.querySelector('.todo-add-plus');
        if (!input || !btn) return;
        btn.disabled = input.value.trim() === '';
        btn.style.opacity = '';
      };
      window.junkieSyncTodoAddButtons = () => {
        document.querySelectorAll('.todo-add-form').forEach(syncTodoAddButton);
      };
      window.junkieSyncTodoAddButtons();
      document.addEventListener('input', (event) => {
        const form = event.target.closest?.('.todo-add-form');
        if (form && event.target.name === 'text') syncTodoAddButton(form);
      });

      // htmx only swaps the todo list/group container, not the add form, so
      // the input keeps focus across a submit. Disable the button while the
      // request is in flight (double-submit guard), then clear the input and
      // restore the button from the input's state -- unlike the old global
      // disable-on-submit guard, which assumed a full page load would reset
      // the button and permanently bricked it after the first htmx add.
      document.body.addEventListener('htmx:beforeRequest', (event) => {
        const btn = event.target.closest?.('.todo-add-form[hx-post]')?.querySelector('.todo-add-plus');
        if (btn) btn.disabled = true;
      });
      document.body.addEventListener('htmx:afterRequest', (event) => {
        const form = event.target.closest?.('.todo-add-form[hx-post]');
        if (!form) return;
        const input = form.querySelector('input[name="text"]');
        if (input && event.detail.successful) {
          input.value = '';
          input.focus();
        }
        syncTodoAddButton(form);
      });

      // Server-backed todo forms navigate on submit, and room pages reload on
      // WebSocket broadcasts, so adding a task normally throws the cursor (and
      // any half-typed text) out of the add box. Stash a short-lived per-tab
      // draft around those reloads and restore it afterwards so tasks can be
      // entered back to back. The guest form has no action attribute: it adds
      // todos without navigating and refocuses itself in guest.js.
      const todoDraftKey = 'junkie:todoDraft';
      let todoFormSubmitting = false;
      const stashTodoDraft = (form, value) => {
        const action = form?.getAttribute('action');
        if (!action) return;
        try {
          sessionStorage.setItem(todoDraftKey, JSON.stringify({ action, value, ts: Date.now() }));
        } catch (_) {}
      };
      // The submit stash must survive until navigation: the server broadcasts
      // the new todo before responding, so this tab's own WebSocket reload can
      // fire while the just-submitted text is still in the input.
      window.junkieStashTypedTodo = () => {
        if (todoFormSubmitting) return;
        const input = document.activeElement;
        if (input?.name === 'text') stashTodoDraft(input.closest?.('.todo-add-form[action]'), input.value);
      };
      document.addEventListener('submit', (event) => {
        const form = event.target.closest?.('.todo-add-form[action]');
        if (!form) return;
        todoFormSubmitting = true;
        stashTodoDraft(form, '');
      }, true);

      const restoreTodoDraft = () => {
        let draft = null;
        try {
          draft = JSON.parse(sessionStorage.getItem(todoDraftKey) || 'null');
          sessionStorage.removeItem(todoDraftKey);
        } catch (_) {}
        if (!draft?.action || Date.now() - (draft.ts || 0) > 15000) return;
        const form = Array.from(document.querySelectorAll('.todo-add-form[action]'))
          .find((el) => el.getAttribute('action') === draft.action);
        const input = form?.querySelector('input[name="text"]');
        if (!input || input.closest('[hidden]')) return;
        input.value = draft.value || '';
        input.focus();
        input.setSelectionRange(input.value.length, input.value.length);
      };
      restoreTodoDraft();

      const copyText = async (text) => {
        try {
          await navigator.clipboard.writeText(text);
          return true;
        } catch {
          try {
            const ta = document.createElement('textarea');
            ta.value = text;
            ta.setAttribute('readonly', '');
            ta.style.position = 'fixed';
            ta.style.left = '-9999px';
            document.body.appendChild(ta);
            ta.select();
            const ok = document.execCommand('copy');
            document.body.removeChild(ta);
            if (ok) return true;
          } catch {}
          window.prompt('Copy this text:', text);
          return false;
        }
      };

      const roomInviteMessage = (path) => {
        const cleanPath = path.startsWith('http') ? new URL(path).pathname : path;
        return 'Join my focus room on junkie: ' + location.origin + cleanPath;
      };

      document.querySelectorAll('.room-share').forEach((share) => {
        const path = share.dataset.roomPath;
        if (!path) return;
        const feedback = share.querySelector('.room-share-feedback');
        let copiedTimer = null;
        const showCopied = () => {
          share.classList.add('is-copied');
          if (feedback) feedback.textContent = 'COPIED ✓';
          if (copiedTimer) clearTimeout(copiedTimer);
          copiedTimer = setTimeout(() => {
            share.classList.remove('is-copied');
            if (feedback) feedback.textContent = '';
            copiedTimer = null;
          }, 1500);
        };
        const wireCopy = (btn, getText) => {
          btn.addEventListener('click', async (event) => {
            event.preventDefault();
            const text = typeof getText === 'function' ? getText() : getText;
            if (!(await copyText(text))) return;
            showCopied();
          });
        };
        share.querySelectorAll('.room-share-copy').forEach((btn) => {
          wireCopy(btn, () => roomInviteMessage(path));
        });
        share.querySelectorAll('.room-share-code[data-copy="code"]').forEach((btn) => {
          wireCopy(btn, () => btn.dataset.roomCode || btn.textContent.trim());
        });
        share.querySelectorAll('.room-share-code:not([data-copy="code"])').forEach((btn) => {
          wireCopy(btn, () => roomInviteMessage(path));
        });
      });

      document.querySelectorAll('[data-open-join]').forEach((link) => {
        link.addEventListener('click', (event) => {
          event.preventDefault();
          const trigger = document.querySelector('.menu-drawer-trigger');
          const drawer = document.querySelector('.menu-drawer');
          const backdrop = document.querySelector('.menu-drawer-backdrop');
          if (!trigger || !drawer) return;
          drawer.classList.add('open');
          backdrop?.removeAttribute('hidden');
          drawer.setAttribute('aria-hidden', 'false');
          trigger.setAttribute('aria-expanded', 'true');
          document.body.classList.add('menu-drawer-open');
          const join = document.getElementById('drawer-join-section');
          if (join) join.open = true;
          join?.querySelector('input')?.focus();
        });
      });

      document.querySelectorAll('form').forEach((form) => {
        form.addEventListener('submit', () => {
          // htmx forms don't navigate away, so a permanent disable here would
          // brick the form after its first use; their in-flight disable is
          // handled by the htmx:beforeRequest/afterRequest pair instead.
          if (form.hasAttribute('hx-post')) return;
          form.querySelectorAll('button[type="submit"], input[type="submit"]').forEach((btn) => {
            btn.disabled = true;
            btn.style.opacity = '0.6';
          });
        });
      });

      document.querySelectorAll('.context-banner-dismiss .banner-dismiss').forEach((btn) => {
        btn.addEventListener('click', () => btn.closest('.context-banner')?.remove());
      });

      document.querySelectorAll('.room-code-input').forEach((input) => {
        const upper = () => {
          const start = input.selectionStart;
          const end = input.selectionEnd;
          input.value = input.value.toUpperCase();
          if (start != null && end != null) input.setSelectionRange(start, end);
        };
        input.addEventListener('input', upper);
      });
    })();
  </script>
</body>
</html>
{{end}}

{{define "heatmap"}}
<div class="heatmap-chart">
  <p class="heatmap-summary">{{focusHours .ActivityTotalMinutes}} hours focused in the last year</p>
  <div class="heatmap-layout">
    <div class="heatmap-dow" aria-hidden="true">
      <span></span><span>Mon</span><span></span><span>Wed</span><span></span><span>Fri</span><span></span>
    </div>
    <div class="heatmap-main">
      <div class="heatmap-months" style="--weeks: {{.ActivityWeeks}}">
        {{range .ActivityMonths}}<span class="heatmap-month" style="--col: {{.Col}}">{{.Label}}</span>{{end}}
      </div>
      <div class="heatmap-wrap">
        <div class="heatmap" style="--weeks: {{.ActivityWeeks}}" aria-label="Focus activity heat map">
          {{range .Activity}}{{if .Empty}}<span class="cell cell-empty"></span>{{else}}<span class="cell l{{.Level}}" title="{{.Date}}: {{.Minutes}} min"></span>{{end}}{{end}}
        </div>
      </div>
    </div>
  </div>
  <div class="heatmap-legend" aria-hidden="true">
    <span>Less</span>
    <span class="cell l0"></span>
    <span class="cell l1"></span>
    <span class="cell l2"></span>
    <span class="cell l3"></span>
    <span class="cell l4"></span>
    <span>More</span>
  </div>
</div>
{{end}}

{{define "todo-remove-icon"}}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg>{{end}}
{{define "todo-restore-icon"}}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 14 4 9l5-5"/><path d="M4 9h11a5 5 0 0 1 0 10h-3"/></svg>{{end}}
{{define "todo-delete-icon"}}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>{{end}}
{{define "copy-icon"}}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16V4a2 2 0 0 1 2-2h10"/></svg>{{end}}
{{define "profile-icon"}}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>{{end}}

{{define "room-membership-pill"}}
{{if .Room.Code}}
<div class="desk-room-bar">
  <a class="room-membership-pill" href="/r/{{.Room.Code}}" data-membership-room="{{.Room.Code}}" data-membership-name="{{.Room.Name}}">
    <span class="room-membership-dot" aria-hidden="true"></span>
    <span class="label room-membership-label">In room</span>
    <span class="room-membership-name">{{.Room.Name}}</span>
  </a>
</div>
{{else if and .Rooms (not .SoloTimer)}}
<div class="desk-room-bar" hidden>
  <a class="room-membership-pill" href="/r/{{(index .Rooms 0).Code}}" data-membership-room="{{(index .Rooms 0).Code}}" data-membership-name="{{(index .Rooms 0).Name}}">
    <span class="room-membership-dot" aria-hidden="true"></span>
    <span class="label room-membership-label">In room</span>
    <span class="room-membership-name">{{(index .Rooms 0).Name}}</span>
  </a>
</div>
{{end}}
{{end}}

{{define "participant-avatar-stack"}}
{{$names := index . "Names"}}
{{$small := index . "Small"}}
{{$max := 5}}
<div class="participant-avatars participant-avatars-stack{{if $small}} participant-avatars-sm{{end}}" aria-label="{{len $names}} focusing">
  {{range $i, $name := $names}}
    {{if lt $i $max}}
    <span class="participant-avatar">{{initial $name}}</span>
    {{end}}
  {{end}}
  {{if gt (len $names) $max}}
  <span class="participant-avatar participant-avatar-overflow" title="{{len $names}} focusing">+{{avatarOverflow (len $names) $max}}</span>
  {{end}}
</div>
{{end}}

{{define "personal-todos-list"}}
<ul class="todo-list" id="personal-todos-list" hx-target="this" hx-swap="outerHTML">
  {{range .Todos}}
    {{template "todo-row" .}}
  {{else}}
    {{if not .HasRooms}}<li class="empty">Nothing yet. Add one thing worth finishing.</li>{{end}}
  {{end}}
</ul>
{{end}}

{{define "todo-row"}}
  <li class="{{if .Removed}}removed{{else if .Done}}done{{end}}">
    {{if .ReadOnly}}
    <span class="todo-avatar" aria-hidden="true">{{initial .DisplayName}}</span>
    {{else}}
    <form method="post" action="/todo/{{.ID}}/toggle" hx-post="/todo/{{.ID}}/toggle"><button type="submit" class="check" aria-label="{{if .Done}}Mark incomplete{{else}}Mark complete{{end}}">{{if .Done}}✓{{else}}○{{end}}</button></form>
    {{end}}
    <span>{{if .ReadOnly}}<span class="todo-text">{{.Text}}</span><span class="todo-sep" aria-hidden="true"> · </span><span class="todo-author">{{.DisplayName}}</span>{{else}}{{.Text}}{{end}}</span>
    {{if .Removed}}
      <div class="todo-actions">
        <form method="post" action="/todo/{{.ID}}/restore" hx-post="/todo/{{.ID}}/restore"><button type="submit" class="todo-action todo-restore" title="Bring back" aria-label="Bring back">{{template "todo-restore-icon" .}}</button></form>
        <form method="post" action="/todo/{{.ID}}/delete" hx-post="/todo/{{.ID}}/delete"><button type="submit" class="todo-action todo-delete" title="Delete permanently" aria-label="Delete permanently">{{template "todo-delete-icon" .}}</button></form>
      </div>
    {{else}}
      <form method="post" action="/todo/{{.ID}}/remove" hx-post="/todo/{{.ID}}/remove"><button type="submit" class="todo-action todo-remove" title="Remove" aria-label="Remove">{{template "todo-remove-icon" .}}</button></form>
    {{end}}
  </li>
{{end}}

{{define "room-timer-status-attrs"}}data-room-sync="{{.Room.Code}}" {{if .Timer}}data-run-id="{{.Timer.ID}}" data-phase="{{.Timer.Phase}}" data-ends-at="{{rfc3339Nano .Timer.PhaseEndsAt}}" data-current-session="{{.Timer.CurrentSession}}" data-total-sessions="{{.Timer.TotalSessions}}" data-participant="{{if .Timer.Participant}}true{{else}}false{{end}}" data-participant-count="{{len .Timer.Participants}}" data-paused="{{if .Timer.PausedAt}}true{{else}}false{{end}}" data-paused-remaining="{{if .Timer.PausedAt}}{{timerSeconds .Timer}}{{else}}0{{end}}"{{else}}data-run-id="" data-phase="idle" data-ends-at="" data-current-session="0" data-total-sessions="0" data-participant="false" data-participant-count="0" data-paused="false" data-paused-remaining="0"{{end}}{{end}}

{{define "todo-row-focus"}}
  <li class="{{if .Done}}done{{end}}">
    <form method="post" action="/todo/{{.ID}}/toggle" hx-post="/todo/{{.ID}}/toggle"><input type="hidden" name="view" value="focus"><button type="submit" class="check" aria-label="{{if .Done}}Mark incomplete{{else}}Mark complete{{end}}">{{if .Done}}✓{{else}}○{{end}}</button></form>
    <span>{{.Text}}</span>
  </li>
{{end}}

{{define "focus-todos-list"}}
<ul class="todo-list" id="focus-todos-list" hx-target="this" hx-swap="outerHTML">
  {{- $hasActive := false -}}
  {{- range . -}}
    {{- if not .Removed -}}
      {{- $hasActive = true -}}
      {{template "todo-row-focus" .}}
    {{- end -}}
  {{- end -}}
  {{- if not $hasActive -}}
    <li class="empty">Nothing yet. Add one thing worth finishing.</li>
  {{- end -}}
</ul>
{{end}}

{{define "todo-row-desk-room"}}
  <li class="{{if .Removed}}removed{{else if .Done}}done{{end}}">
    {{if .ReadOnly}}
    <span class="todo-avatar" aria-hidden="true">{{initial .DisplayName}}</span>
    {{else}}
    <form method="post" action="/todo/{{.ID}}/toggle" hx-post="/todo/{{.ID}}/toggle">
      <input type="hidden" name="desk" value="1">
      <input type="hidden" name="room" value="{{.RoomCode}}">
      <button type="submit" class="check" aria-label="{{if .Done}}Mark incomplete{{else}}Mark complete{{end}}">{{if .Done}}✓{{else}}○{{end}}</button>
    </form>
    {{end}}
    <span>{{if .ReadOnly}}<span class="todo-text">{{.Text}}</span><span class="todo-sep" aria-hidden="true"> · </span><span class="todo-author">{{.DisplayName}}</span>{{else}}{{.Text}}{{end}}</span>
    {{if .Removed}}
      <div class="todo-actions">
        <form method="post" action="/todo/{{.ID}}/restore" hx-post="/todo/{{.ID}}/restore">
          <input type="hidden" name="desk" value="1">
          <input type="hidden" name="room" value="{{.RoomCode}}">
          <button type="submit" class="todo-action todo-restore" title="Bring back" aria-label="Bring back">{{template "todo-restore-icon" .}}</button>
        </form>
        <form method="post" action="/todo/{{.ID}}/delete" hx-post="/todo/{{.ID}}/delete">
          <input type="hidden" name="desk" value="1">
          <input type="hidden" name="room" value="{{.RoomCode}}">
          <button type="submit" class="todo-action todo-delete" title="Delete permanently" aria-label="Delete permanently">{{template "todo-delete-icon" .}}</button>
        </form>
      </div>
    {{else}}
      <form method="post" action="/todo/{{.ID}}/remove" hx-post="/todo/{{.ID}}/remove">
        <input type="hidden" name="desk" value="1">
        <input type="hidden" name="room" value="{{.RoomCode}}">
        <button type="submit" class="todo-action todo-remove" title="Remove" aria-label="Remove">{{template "todo-remove-icon" .}}</button>
      </form>
    {{end}}
  </li>
{{end}}

{{define "todo-groups-desk-room"}}
<div class="todo-groups" data-room="{{.RoomCode}}" id="todo-groups-{{.RoomCode}}" hx-target="this" hx-swap="outerHTML">
  <section class="todo-group" data-group="mine">
    <button type="button" class="todo-group-toggle" aria-expanded="true" aria-controls="todo-group-mine-{{.RoomCode}}">
      <svg class="todo-group-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg>
      <span class="label">{{if .UserName}}{{.UserName}}{{else}}You{{end}}</span>
    </button>
    <ul class="todo-list" id="todo-group-mine-{{.RoomCode}}">
      {{range .Mine}}
        {{template "todo-row-desk-room" .}}
      {{else}}
        <li class="empty">Nothing here yet.</li>
      {{end}}
    </ul>
  </section>
  {{if .Others}}
  <section class="todo-group" data-group="others">
    <button type="button" class="todo-group-toggle" aria-expanded="true" aria-controls="todo-group-others-{{.RoomCode}}">
      <svg class="todo-group-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg>
      <span class="label">Everyone else</span>
    </button>
    <ul class="todo-list" id="todo-group-others-{{.RoomCode}}">
      {{range .Others}}
        {{template "todo-row-desk-room" .}}
      {{end}}
    </ul>
  </section>
  {{end}}
</div>
{{end}}

{{define "todo-groups-room"}}
<div class="todo-groups" data-room="{{.RoomCode}}" id="todo-groups-{{.RoomCode}}" hx-target="this" hx-swap="outerHTML">
  <section class="todo-group" data-group="mine">
    <button type="button" class="todo-group-toggle" aria-expanded="true" aria-controls="todo-group-mine-{{.RoomCode}}">
      <svg class="todo-group-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg>
      <span class="label">{{if .UserName}}{{.UserName}}{{else}}You{{end}}</span>
    </button>
    <ul class="todo-list" id="todo-group-mine-{{.RoomCode}}">
      {{range .Mine}}
        {{template "todo-row" .}}
      {{else}}
        <li class="empty">Nothing here yet.</li>
      {{end}}
    </ul>
  </section>
  {{if .Others}}
  <section class="todo-group" data-group="others">
    <button type="button" class="todo-group-toggle" aria-expanded="true" aria-controls="todo-group-others-{{.RoomCode}}">
      <svg class="todo-group-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg>
      <span class="label">Everyone else</span>
    </button>
    <ul class="todo-list" id="todo-group-others-{{.RoomCode}}">
      {{range .Others}}
        {{template "todo-row" .}}
      {{end}}
    </ul>
  </section>
  {{end}}
</div>
{{end}}

{{define "auth-back"}}
<a class="auth-back" href="/" aria-label="Back to home">
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 18l-6-6 6-6"/></svg>
</a>
{{end}}

{{define "auth-brand"}}
<div class="auth-brand"><span class="brand-mark">j</span></div>
{{end}}

{{define "password-field"}}
<span class="password-field">
  <input type="password" name="{{.Name}}" autocomplete="{{.Autocomplete}}" required>
  <button type="button" class="password-toggle" aria-label="Show password" aria-pressed="false">
    <svg class="icon-eye" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z"/><circle cx="12" cy="12" r="3"/></svg>
    <svg class="icon-eye-off" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 3l18 18"/><path d="M10.6 10.6a3 3 0 0 0 4.24 4.24"/><path d="M9.9 5.1A10.4 10.4 0 0 1 12 5c6.5 0 10 7 10 7a13.3 13.3 0 0 1-3.09 3.9M6.1 6.1A13.4 13.4 0 0 0 2 12s3.5 7 10 7c1.15 0 2.25-.16 3.29-.47"/></svg>
  </button>
</span>
{{end}}

{{define "desk-private-timer"}}
<div class="desk-timer-view" data-mode="private"{{if .Rooms}} hidden{{end}}>
  {{if .SoloTimer}}
  <article class="timer-card panel desk-timer-card {{.SoloTimer.Phase}} solo-timer">
    {{if eq .SoloTimer.Phase "focus"}}
    <p class="label label-accent">Private focus</p>
    <div class="circle-timer running" role="timer" aria-label="Private focus countdown">
      <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
        <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
        <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" stroke-dashoffset="0"/>
      </svg>
      <div class="circle-timer-core">
        <div class="circle-timer-countdown countdown" aria-live="polite" data-seconds="{{secondsUntil .SoloTimer.PhaseEndsAt}}" data-total="{{mul .SoloTimer.FocusMinutes 60}}">--:--</div>
      </div>
    </div>
    <form method="post" action="/solo/cancel" onsubmit="return confirm('End this focus session? It won\'t count toward your map.')">
      <button type="submit" class="btn-ghost timer-cancel">End early</button>
    </form>
    {{else if soloBreakPending .SoloTimer}}
    <p class="label label-warn">Private break ready</p>
    <div class="room-ready-time mono">{{.SoloTimer.BreakMinutes}}:00</div>
    <form method="post" action="/solo/break/start"><button type="submit" class="btn-primary">Start break</button></form>
    <form method="post" action="/solo/break/skip"><button type="submit" class="btn-ghost timer-cancel">Skip break &amp; continue</button></form>
    {{else}}
    <p class="label label-warn">Private break</p>
    <div class="circle-timer break-running breather" role="timer" aria-label="Private break countdown">
      <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
        <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
        <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" stroke-dashoffset="0"/>
      </svg>
      <div class="circle-timer-core">
        <div class="circle-timer-countdown countdown" aria-live="polite" data-seconds="{{secondsUntil .SoloTimer.PhaseEndsAt}}" data-total="{{mul .SoloTimer.BreakMinutes 60}}">--:--</div>
      </div>
    </div>
    <form method="post" action="/solo/break/skip"><button type="submit" class="btn-ghost timer-cancel">Skip break &amp; continue</button></form>
    {{end}}
  </article>
  {{else}}
  <article class="circle-timer-wrap timer-card panel idle desk-timer-card">
    <form class="circle-timer-form" method="post" action="/solo/start">
      <div class="circle-timer idle" role="group" aria-label="Set private focus duration">
        <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
          <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
          <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" stroke-dashoffset="0"/>
        </svg>
        <div class="circle-timer-core">
          <button type="button" class="circle-timer-step" data-delta="-5" aria-label="Decrease 5 minutes">−</button>
          <label class="circle-timer-time"><input type="number" name="focus_minutes" min="5" max="180" value="50" aria-label="Focus minutes"></label>
          <button type="button" class="circle-timer-step" data-delta="5" aria-label="Increase 5 minutes">+</button>
        </div>
      </div>
    </form>
    <p class="label desk-ring-hint">Scroll ±1 · buttons ±5 · tap ring to focus</p>
  </article>
  {{end}}
</div>
{{end}}

{{define "desk-room-timer"}}
<div class="desk-timer-view" data-mode="room" data-room="{{.Room.Code}}" {{template "room-timer-status-attrs" .}} hidden>
  {{if .Timer}}
  <article class="timer-card panel desk-timer-card {{.Timer.Phase}}">
    <p class="label {{if or (eq .Timer.Phase "focus") (eq .Timer.Phase "lobby")}}label-accent{{else}}label-warn{{end}}">
      {{if eq .Timer.Phase "lobby"}}Starting · join now{{else if eq .Timer.Phase "focus"}}Focus · session {{.Timer.CurrentSession}} of {{.Timer.TotalSessions}}{{else if .Timer.PausedAt}}Break paused{{else}}Break · session {{.Timer.CurrentSession}} of {{.Timer.TotalSessions}}{{end}}
    </p>
    <div class="circle-timer {{if or (eq .Timer.Phase "focus") (eq .Timer.Phase "lobby")}}running{{else}}break-running breather{{end}}" role="timer" aria-label="{{.Timer.Phase}} countdown">
      <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
        <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
        <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" stroke-dashoffset="0"/>
      </svg>
      <div class="circle-timer-core">
        <div class="circle-timer-countdown countdown" aria-live="polite" data-seconds="{{timerSeconds .Timer}}" data-total="{{if eq .Timer.Phase "lobby"}}10{{else if eq .Timer.Phase "focus"}}{{mul .Timer.FocusMinutes 60}}{{else}}{{mul .Timer.BreakMinutes 60}}{{end}}" data-paused="{{if .Timer.PausedAt}}true{{else}}false{{end}}">--:--</div>
      </div>
    </div>
    {{if and (eq .Timer.Phase "focus") (not .Timer.Participant)}}
    <p class="label label-warn">Watching · join on next break</p>
    {{end}}
    <footer class="desk-timer-footer">
      {{if and (or (eq .Timer.Phase "lobby") (eq .Timer.Phase "break")) (not .Timer.Participant)}}
      <form method="post" action="/r/{{.Room.Code}}/timer-join">
        <input type="hidden" name="next" value="/?todos=room&amp;room={{.Room.Code}}">
        <button type="submit" class="btn-primary">Join this block</button>
      </form>
      {{end}}
      {{if eq .Timer.Phase "break"}}
      <form method="post" action="/r/{{.Room.Code}}/{{if .Timer.PausedAt}}timer-resume{{else}}timer-pause{{end}}">
        <input type="hidden" name="next" value="/?todos=room&amp;room={{.Room.Code}}">
        <button type="submit" class="{{if .Timer.PausedAt}}btn-primary{{else}}btn-ghost{{end}}">{{if .Timer.PausedAt}}Resume break{{else}}Pause break{{end}}</button>
      </form>
      <form method="post" action="/r/{{.Room.Code}}/timer-skip-break">
        <input type="hidden" name="next" value="/?todos=room&amp;room={{.Room.Code}}">
        <button type="submit" class="btn-ghost timer-cancel">Skip break &amp; continue</button>
      </form>
      {{end}}
      {{if .Timer.Participant}}
      <form method="post" action="/r/{{.Room.Code}}/timer-leave">
        <input type="hidden" name="next" value="/?todos=room&amp;room={{.Room.Code}}">
        <button type="submit" class="btn-ghost timer-cancel">Leave this focus block</button>
      </form>
      {{end}}
      {{if gt (len .Timer.Participants) 0}}{{template "participant-avatar-stack" dict "Names" .Timer.Participants "Small" true}}{{end}}
      <p class="label">{{if .Timer.Participant}}Participating{{else}}Watching{{end}} · {{len .Timer.Participants}} joined</p>
    </footer>
  </article>
  {{else}}
  <article class="circle-timer-wrap timer-card panel idle desk-timer-card">
    <form class="circle-timer-form" method="post" action="/r/{{.Room.Code}}/timer-start" data-room-name="{{.Room.Name}}">
      <input type="hidden" name="next" value="/?todos=room&amp;room={{.Room.Code}}">
      <div class="circle-timer idle room-desk-timer" role="group" aria-label="Start {{.Room.Name}} focus timer">
        <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
          <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
          <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" stroke-dashoffset="0"/>
        </svg>
        <div class="circle-timer-core">
          <button type="button" class="circle-timer-step" data-delta="-5" aria-label="Decrease room focus by 5 minutes">−</button>
          <label class="circle-timer-time"><input type="number" name="focus_minutes" min="5" max="180" value="{{.Room.FocusMinutes}}" aria-label="Room focus minutes"></label>
          <button type="button" class="circle-timer-step" data-delta="5" aria-label="Increase room focus by 5 minutes">+</button>
        </div>
      </div>
    </form>
    <p class="label desk-ring-hint">Scroll ±1 · buttons ±5 · tap ring to start room</p>
  </article>
  {{end}}
</div>
{{end}}

{{define "login"}}{{template "shell" .}}{{end}}
{{define "dashboard"}}{{template "shell" .}}{{end}}
{{define "profile"}}{{template "shell" .}}{{end}}
{{define "room"}}{{template "shell" .}}{{end}}
{{define "admin"}}{{template "shell" .}}{{end}}
{{define "forbidden"}}{{template "shell" .}}{{end}}

{{define "room-invite"}}{{template "shell" .}}{{end}}

{{define "menu-drawer-guest"}}
<div class="menu-drawer-backdrop" hidden></div>
<aside class="menu-drawer" aria-hidden="true">
  <div class="menu-drawer-head">
    <button type="button" class="menu-drawer-close" aria-label="Close">×</button>
  </div>
  <div class="drawer-identity">
    <a href="/profile" class="drawer-profile-row">
      <span class="drawer-avatar">{{template "profile-icon" .}}</span>
      <span class="drawer-name">Profile</span>
    </a>
    <a href="/login{{if .Next}}?next={{.Next}}{{end}}" class="btn-primary drawer-signin">Sign in</a>
    <p class="label drawer-guest-hint">Rooms need an account — sign in to study together.</p>
  </div>
  <details class="drawer-details" id="drawer-join-section">
    <summary><span class="label">Join room</span><svg class="drawer-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg></summary>
    <form class="room-join" method="post" action="/rooms/join-intent">
      <input type="hidden" name="next" value="{{if .Room.Code}}/r/{{.Room.Code}}{{else if eq .Title "Profile"}}/profile{{else}}/{{end}}">
      <input class="room-code-input" name="code" placeholder="Code or link" required aria-label="Room code" autocapitalize="characters" spellcheck="false">
      <button type="submit" class="btn-primary btn-compact">Join</button>
    </form>
  </details>
</aside>
{{end}}

{{define "menu-drawer-user"}}
<div class="menu-drawer-backdrop" hidden></div>
<aside class="menu-drawer" aria-hidden="true">
  <div class="menu-drawer-head">
    <button type="button" class="menu-drawer-close" aria-label="Close">×</button>
  </div>
  <div class="drawer-identity drawer-identity-user">
    <span class="drawer-avatar">{{initial .User.DisplayName}}</span>
    <div class="drawer-user-meta">
      <span class="drawer-name">{{.User.DisplayName}}</span>
      <a href="/profile" class="label drawer-profile-link">View profile</a>
    </div>
  </div>
  <p class="label drawer-section-label">Your rooms</p>
  <div class="room-list">
    {{range .Rooms}}
      <a class="room-row{{if eq $.Room.Code .Code}} is-here{{end}}" href="/r/{{.Code}}">
        <div class="room-row-main">
          <strong>{{.Name}}</strong>
          <span class="mono room-row-code">{{.Code}} · {{.AutoSessions}}×{{.FocusMinutes}}/{{.BreakMinutes}}</span>
        </div>
        {{if eq $.Room.Code .Code}}<span class="label here-tag">Here</span>{{end}}
      </a>
    {{else}}
      <p class="empty">No rooms yet.</p>
    {{end}}
  </div>
  <details class="drawer-details">
    <summary><span class="label">Create room</span><svg class="drawer-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg></summary>
    <form class="room-create" method="post" action="/rooms">
      <input name="name" placeholder="Room name (optional)">
      <button type="submit" class="btn-primary btn-compact">Create</button>
    </form>
  </details>
  <details class="drawer-details" id="drawer-join-section">
    <summary><span class="label">Join room</span><svg class="drawer-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg></summary>
    <form class="room-join" method="post" action="/rooms/join">
      <input type="hidden" name="next" value="{{if .Room.Code}}/r/{{.Room.Code}}{{else if eq .Title "Profile"}}/profile{{else}}/{{end}}">
      <input class="room-code-input" name="code" placeholder="Code or link" required aria-label="Room code" autocapitalize="characters" spellcheck="false">
      <button type="submit" class="btn-primary btn-compact">Join</button>
    </form>
  </details>
  <form class="menu-drawer-logout" method="post" action="/logout">
    <button type="submit" class="btn-ghost">Log out</button>
  </form>
</aside>
{{end}}

{{define "content"}}
  {{if or (eq .Title "Sign in") (eq .Title "Sign up")}}
    <section class="auth-card">
      <div class="auth-card-top">
        {{template "auth-back" .}}
        {{template "auth-brand" .}}
      </div>
      {{if .AuthSignup}}
      <h1 class="auth-title">Start focusing</h1>
      {{else}}
      <h1 class="auth-title">Welcome back</h1>
      {{end}}
      {{if .AuthBanner}}<p class="context-banner">{{.AuthBanner}}</p>{{end}}
      {{if .Error}}<p class="notice notice-error">{{.Error}}</p>{{end}}
      {{if .Notice}}<p class="notice-ok" role="status">{{.Notice}}</p>{{end}}
      {{if .AuthSignup}}
      <form class="stack auth-form" method="post" action="/signup">
        {{if .Next}}<input type="hidden" name="next" value="{{.Next}}">{{end}}
        <label>Username <input name="username" autocomplete="username" required></label>
        <label>Password {{template "password-field" (dict "Name" "password" "Autocomplete" "new-password")}}</label>
        <button type="submit" class="btn-primary">Create account</button>
      </form>
      <p class="muted auth-switch">Already have an account? <a href="/login{{if .Next}}?next={{.Next}}{{end}}">Sign in</a></p>
      {{else}}
      <form class="stack auth-form" method="post" action="/login">
        {{if .Next}}<input type="hidden" name="next" value="{{.Next}}">{{end}}
        <label>Username <input name="username" autocomplete="username" required></label>
        <label>Password {{template "password-field" (dict "Name" "password" "Autocomplete" "current-password")}}</label>
        <button type="submit" class="btn-primary">Sign in</button>
      </form>
      <p class="muted auth-switch">New here? <a href="/login?mode=signup{{if .Next}}&amp;next={{.Next}}{{end}}">Create an account</a></p>
      {{end}}
    </section>
  {{else if eq .Title "Admin"}}
    <div class="admin-shell">
      <header class="admin-header">
        <div>
          <p class="eyebrow">Platform operations</p>
          <h1>Admin space</h1>
          <p class="muted">Operational metadata only. Private todos and detailed activity are never shown here.</p>
        </div>
        <a href="/" class="btn btn-ghost btn-compact">Back to app</a>
      </header>
      {{if .Error}}<p class="notice notice-error" role="alert">{{.Error}}</p>{{end}}
      <section class="admin-stats" aria-label="Platform overview">
        <article class="panel"><span class="label">Users</span><strong>{{.Admin.Overview.Users}}</strong></article>
        <article class="panel"><span class="label">Rooms</span><strong>{{.Admin.Overview.Rooms}}</strong></article>
        <article class="panel"><span class="label">Active room timers</span><strong>{{.Admin.Overview.ActiveRoomTimers}}</strong></article>
        <article class="panel"><span class="label">Total focus minutes</span><strong>{{.Admin.Overview.TotalFocusMinutes}}</strong></article>
      </section>
      <section class="panel admin-section">
        <div class="panel-title">
          <div><p class="eyebrow">Accounts</p><h2>Users</h2></div>
          <span class="role-badge role-{{.User.Role}}">{{.User.Role}}</span>
        </div>
        <div class="admin-table-wrap">
          <table class="admin-table">
            <thead><tr><th scope="col">Username</th><th scope="col">Role</th><th scope="col">Joined</th><th scope="col">Rooms</th>{{if .Admin.IsOwner}}<th scope="col">Focus summary</th><th scope="col">Role action</th>{{end}}</tr></thead>
            <tbody>
            {{range .Admin.Users}}
              <tr>
                <td><strong>{{.Username}}</strong></td>
                <td><span class="role-badge role-{{.Role}}">{{.Role}}</span></td>
                <td><time datetime="{{.JoinedAt.Format "2006-01-02"}}">{{.JoinedAt.Format "Jan 2, 2006"}}</time></td>
                <td>{{.RoomsCount}}</td>
                {{if $.Admin.IsOwner}}
                  <td>{{.FocusMinutes}} min{{if .LastActivityAt}} · last {{.LastActivityAt.Format "Jan 2, 2006"}}{{else}} · no activity{{end}}</td>
                  <td>
                    {{if eq .Role "user"}}
                    <form method="post" action="/admin/users/{{.ID}}/role" onsubmit="return confirm('Promote {{.Username}} to admin?')">
                      <input type="hidden" name="role" value="admin">
                      <button type="submit" class="btn-ghost btn-compact">Promote</button>
                    </form>
                    {{else if eq .Role "admin"}}
                    <form method="post" action="/admin/users/{{.ID}}/role" onsubmit="return confirm('Demote {{.Username}} to user?')">
                      <input type="hidden" name="role" value="user">
                      <button type="submit" class="btn-ghost btn-compact">Demote</button>
                    </form>
                    {{else}}<span class="muted">Protected</span>{{end}}
                  </td>
                {{end}}
              </tr>
            {{else}}
              <tr><td colspan="6" class="empty">No users found.</td></tr>
            {{end}}
            </tbody>
          </table>
        </div>
      </section>
      <section class="panel admin-section">
        <div class="panel-title"><div><p class="eyebrow">Collaboration</p><h2>Rooms</h2></div></div>
        <div class="admin-table-wrap">
          <table class="admin-table">
            <thead><tr><th scope="col">Room</th><th scope="col">Code</th><th scope="col">Creator</th><th scope="col">Members</th><th scope="col">Status</th><th scope="col">Created</th><th scope="col">Action</th></tr></thead>
            <tbody>
            {{range .Admin.Rooms}}
              <tr>
                <td><strong>{{.Name}}</strong></td>
                <td><span class="mono">{{.Code}}</span></td>
                <td>{{.Creator}}</td>
                <td>{{.MembersCount}}</td>
                <td>{{if .Active}}<span class="status-active">Active timer</span>{{else}}Idle{{end}}</td>
                <td><time datetime="{{.CreatedAt.Format "2006-01-02"}}">{{.CreatedAt.Format "Jan 2, 2006"}}</time></td>
                <td>
                  <form method="post" action="/admin/rooms/{{.ID}}/delete" onsubmit="return confirm('Permanently delete room {{.Name}} and its room data?')">
                    <button type="submit" class="btn-danger btn-compact">Delete</button>
                  </form>
                </td>
              </tr>
            {{else}}
              <tr><td colspan="7" class="empty">No rooms found.</td></tr>
            {{end}}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  {{else if eq .Title "Access denied"}}
    <section class="auth-card forbidden-card">
      <p class="eyebrow">403 · Forbidden</p>
      <h1>Access denied</h1>
      <p class="muted">{{.ForbiddenMessage}}</p>
      <a href="/" class="btn btn-primary">Return to junkie</a>
    </section>
  {{else if eq .Title "Dashboard"}}
    {{if .Error}}<p class="context-banner context-banner-dismiss" role="status">{{.Error}} <button type="button" class="banner-dismiss" aria-label="Dismiss">×</button></p>{{end}}
    {{if .GuestMode}}
    <div id="guest-desk" class="desk-shell"></div>
    <script src="/assets/guest.js"></script>
    {{else}}
    {{if and .SoloTimer (not .SoloTimer)}}
    <div class="desk-shell desk-shell-focus">
    <section class="focus-desk solo-timer">
      <div class="focus-desk-main">
        {{if eq .SoloTimer.Phase "focus"}}
        <p class="label label-accent">{{.SoloTimer.FocusMinutes}} min focus</p>
        <article class="circle-timer-wrap">
          <div class="circle-timer running" role="timer" aria-label="Focus countdown">
            <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
              <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
              <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" stroke-dashoffset="0"/>
            </svg>
            <div class="circle-timer-core">
              <div class="circle-timer-countdown countdown" aria-live="polite" data-seconds="{{secondsUntil .SoloTimer.PhaseEndsAt}}" data-total="{{mul .SoloTimer.FocusMinutes 60}}">--:--</div>
            </div>
          </div>
          <form method="post" action="/solo/cancel" onsubmit="return confirm('End this focus session? It won\'t count toward your map.')">
            <button type="submit" class="btn-ghost timer-cancel">End early</button>
          </form>
        </article>
        {{else if soloBreakPending .SoloTimer}}
        <p class="label label-warn">{{.SoloTimer.BreakMinutes}} min break</p>
        <article class="circle-timer-wrap">
          <div class="circle-timer break-offer breather" role="timer" aria-label="Break ready">
            <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
              <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
              <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" stroke-dashoffset="0" style="stroke-dashoffset: 0"/>
            </svg>
            <div class="circle-timer-core">
              <div class="circle-timer-countdown countdown" aria-live="polite">{{printf "%02d:%02d" .SoloTimer.BreakMinutes 0}}</div>
            </div>
          </div>
          <form method="post" action="/solo/break/start">
            <button type="submit" class="btn-primary timer-cancel">Start break</button>
          </form>
          <form method="post" action="/solo/break/skip">
            <button type="submit" class="btn-ghost timer-cancel">Skip break &amp; continue</button>
          </form>
        </article>
        {{else}}
        <p class="label label-warn">{{.SoloTimer.BreakMinutes}} min break</p>
        <article class="circle-timer-wrap">
          <div class="circle-timer break-running breather" role="timer" aria-label="Break countdown">
            <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
              <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
              <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" stroke-dashoffset="0"/>
            </svg>
            <div class="circle-timer-core">
              <div class="circle-timer-countdown countdown" aria-live="polite" data-seconds="{{secondsUntil .SoloTimer.PhaseEndsAt}}" data-total="{{mul .SoloTimer.BreakMinutes 60}}">--:--</div>
            </div>
          </div>
          <form method="post" action="/solo/break/skip">
            <button type="submit" class="btn-ghost timer-cancel">Skip break &amp; continue</button>
          </form>
        </article>
        {{end}}
      </div>
      <div class="focus-todos-backdrop" hidden></div>
      <button type="button" class="focus-todos-toggle" aria-expanded="false" aria-controls="focus-todos-panel">Todos</button>
      <aside class="focus-todos-panel" id="focus-todos-panel" aria-hidden="true">
        <button type="button" class="focus-todos-pin" aria-label="Pin todos panel" title="Pin panel"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M12 17v5M9 3h6l1 7h4l-5 6v5H9v-5L4 10h4z"/></svg></button>
        <div class="panel-title">
          <h2>Todos</h2>
        </div>
        {{template "focus-todos-list" .PersonalTodos}}
      </aside>
    </section>
    </div>
    {{else}}
    <div class="desk-shell">
    {{template "room-membership-pill" .}}
    {{range .Rooms}}<span hidden data-room-ws="{{.Code}}" data-room-name="{{.Name}}"></span>{{end}}
      <section class="grid two desk-grid">
        <div class="desk-ring-column">
        {{template "desk-private-timer" .}}
        {{range .DeskRoomTodos}}{{template "desk-room-timer" .}}{{end}}
        </div>

        <article class="panel desk-todos-panel"{{if .DeskRoomTodos}} data-has-rooms="true"{{end}}>
          <div class="panel-title desk-todos-head">
            {{if .DeskRoomTodos}}
            <div class="desk-todos-switch">
              <button type="button" class="desk-todos-mode" aria-haspopup="listbox" aria-expanded="false">
                <span class="desk-todos-mode-label">Room todos</span>
                <svg class="desk-todos-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg>
              </button>
              <div class="desk-todos-menu" hidden role="listbox">
                <button type="button" role="option" data-mode="private">Private</button>
                {{range .DeskRoomTodos}}
                <button type="button" role="option" data-mode="room" data-room="{{.Room.Code}}" data-room-name="{{.Room.Name}}">{{.Room.Name}}</button>
                {{end}}
              </div>
            </div>
            {{else}}
            <h2>Private todos</h2>
            <p class="desk-join-link"><a href="#" data-open-join class="mono-link">Have a room code?</a></p>
            {{end}}
            {{if .DeskRoomTodos}}<span class="label desk-todos-hint label-warn">Public to the room</span>{{end}}
          </div>
          <div class="desk-todos-view" data-mode="private"{{if .DeskRoomTodos}} hidden{{end}}>
            <form class="inline-form todo-add-form" method="post" action="/todos" hx-post="/todos" hx-target="#personal-todos-list" hx-swap="outerHTML">
              <input name="text" placeholder="What do you need to do?" required>
              <button type="submit" class="todo-add-plus" aria-label="Add task">+</button>
            </form>
            {{template "personal-todos-list" (dict "Todos" .PersonalTodos "HasRooms" (gt (len .DeskRoomTodos) 0))}}
            {{if and .DeskRoomTodos (not .Rooms)}}
            <p class="desk-join-link"><a href="#" class="btn btn-ghost btn-compact" onclick="document.querySelector('.menu-drawer-trigger')?.click();return false">Create a room</a></p>
            {{end}}
          </div>
          {{range .DeskRoomTodos}}
          <div class="desk-todos-view" data-mode="room" data-room="{{.Room.Code}}" hidden>
            <form class="inline-form todo-add-form" method="post" action="/r/{{.Room.Code}}/todos" hx-post="/r/{{.Room.Code}}/todos" hx-target="#todo-groups-{{.Room.Code}}" hx-swap="outerHTML">
              <input type="hidden" name="next" value="/?todos=room&amp;room={{.Room.Code}}">
              <input type="hidden" name="desk" value="1">
              <input name="text" placeholder="What are you working on?" required>
              <button type="submit" class="todo-add-plus" aria-label="Add task">+</button>
            </form>
            {{template "todo-groups-desk-room" (.Grouped.View .Room.Code $.User.DisplayName)}}
          </div>
          {{end}}
        </article>
      </section>

    </div>
    {{end}}
    {{end}}
  {{else if eq .Title "Profile"}}
    <section class="panel profile-page">
      <div class="panel-title">
        <h1>Profile</h1>
        {{if .GuestMode}}
          <span>Stored on this device</span>
        {{else}}
          <span>{{.User.DisplayName}}</span>
        {{end}}
      </div>
      {{if .GuestMode}}
        <div id="guest-profile-work-map"></div>
        <script src="/assets/guest.js"></script>
      {{else}}
        {{if .Error}}<p class="notice-error" role="alert">{{.Error}}</p>{{end}}
        {{if .Notice}}<p class="notice-ok" role="status">{{.Notice}}</p>{{end}}
        <div class="profile-stats">
          <div>
            <strong>{{focusHours .ActivityTotalMinutes}}</strong>
            <span>focus hours</span>
          </div>
          <div>
            <strong>{{len .Rooms}}</strong>
            <span>rooms joined</span>
          </div>
        </div>
        {{template "heatmap" .}}
        <div class="profile-preference">
          <div>
            <strong>Room invite notifications</strong>
            <p class="muted">Notify me when a room starts a focus lobby while junkie is in the background.</p>
          </div>
          <label class="toggle-control">
            <input type="checkbox" id="room-invite-notifications" aria-label="Room invite notifications">
            <span aria-hidden="true"></span>
          </label>
        </div>
        <div class="profile-preference profile-password">
          <div>
            <strong>Change password</strong>
            <p class="muted">Updating your password signs out all other devices, so anyone else using your account loses access.</p>
          </div>
        </div>
        <form class="profile-password-form" method="post" action="/profile/password">
          <label>Current password <input type="password" name="current_password" autocomplete="current-password" required></label>
          <label>New password <input type="password" name="new_password" autocomplete="new-password" required minlength="4"></label>
          <label>Confirm new password <input type="password" name="confirm_password" autocomplete="new-password" required minlength="4"></label>
          <button type="submit" class="btn-primary btn-compact">Update password</button>
        </form>
        <p class="muted profile-follow-stub">Follow friends — coming soon</p>
        {{if ne .User.Role "owner"}}
        <div class="profile-preference profile-danger">
          <div>
            <strong>Delete account</strong>
            <p class="muted">Permanently deletes your account, any rooms you created, your todos, and your activity history. This cannot be undone.</p>
          </div>
        </div>
        <form class="profile-delete-form" method="post" action="/profile/delete" onsubmit="return confirm('Permanently delete your account and all its data? This cannot be undone.')">
          <label>Confirm your password <input type="password" name="password" autocomplete="current-password" required></label>
          <button type="submit" class="btn-danger btn-compact">Delete account</button>
        </form>
        {{end}}
      {{end}}
    </section>
  {{else if eq .Title "Join room"}}
    <section class="auth-card room-invite-card">
      <div class="auth-card-top">
        {{template "auth-back" .}}
        {{template "auth-brand" .}}
      </div>
      <p class="eyebrow">Room invite</p>
      <h1>Join {{.Room.Name}}?</h1>
      <p class="muted">You were invited to a focus room. Room code: <strong>{{.Room.Code}}</strong></p>
      <form class="stack room-invite-actions" method="post" action="/join/confirm">
        <input type="hidden" name="code" value="{{.Room.Code}}">
        <button type="submit" name="action" value="join">Join room</button>
        <button type="submit" name="action" value="cancel" class="ghost">Cancel</button>
      </form>
    </section>
  {{else}}
    <span hidden data-room-ws="{{.Room.Code}}" data-room-name="{{.Room.Name}}"></span>
    {{if .FocusMode}}
    <div class="room-focus-page" data-room-page {{template "room-timer-status-attrs" .}}>
    {{template "room-membership-pill" .}}
    <section class="room-focus-shell">
      <p class="label label-accent">Focus · session {{.Timer.CurrentSession}} of {{.Timer.TotalSessions}}</p>
      <p class="room-focus-name">{{.Room.Name}}</p>
      <div class="circle-timer running room-focus-ring" role="timer" aria-label="Focus countdown">
        <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
          <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
          <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" stroke-dashoffset="0"/>
        </svg>
        <div class="circle-timer-core">
          <div class="circle-timer-countdown countdown" aria-live="polite" data-seconds="{{secondsUntil .Timer.PhaseEndsAt}}" data-total="{{mul .Timer.FocusMinutes 60}}">--:--</div>
        </div>
      </div>
      {{if gt (len .Timer.Participants) 1}}{{template "participant-avatar-stack" dict "Names" .Timer.Participants "Small" false}}{{end}}
      <p class="label">{{if eq (len .Timer.Participants) 1}}Focusing solo{{else}}{{len .Timer.Participants}} focusing{{end}}</p>
      {{if .Timer.Participant}}
      <form method="post" action="/r/{{.Room.Code}}/timer-leave">
        <button type="submit" class="btn-ghost timer-cancel">Leave focus block</button>
      </form>
      {{end}}
    </section>
    </div>
    {{else}}
    <section class="room-shell" data-room-page {{template "room-timer-status-attrs" .}}>
      <div class="room-header-new">
        <div class="room-header-main">
          <div class="label label-accent room-eyebrow">
            <span class="room-eyebrow-label">Room ·</span>
            <span class="room-share" data-room-path="/r/{{.Room.Code}}">
              <button type="button" class="copy-chip mono room-share-code" data-copy="code" data-room-code="{{.Room.Code}}" aria-label="Copy room code {{.Room.Code}}" title="Copy room code">{{.Room.Code}}</button>
              <button type="button" class="room-share-copy room-share-copy-icon" aria-label="Copy invite link" title="Copy invite link">{{template "copy-icon" .}}</button>
              <span class="room-share-feedback" aria-live="polite"></span>
            </span>
          </div>
          <div class="room-title-row">
            <h1 class="room-name-display" id="room-name-display">{{.Room.Name}}</h1>
            <button type="button" class="room-rename-trigger" aria-label="Rename room"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M12 20h9M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z"/></svg></button>
          </div>
          <form class="room-rename-form" id="room-rename-form" method="post" action="/r/{{.Room.Code}}/rename" hidden>
            <input name="name" value="{{.Room.Name}}" aria-label="Room name" required>
            <button type="submit" class="btn-primary btn-compact">Save</button>
            <button type="button" class="btn-ghost btn-compact room-rename-cancel">Cancel</button>
          </form>
        </div>
        <div class="room-members-meta">
          {{if and .Timer (gt (len .Timer.Participants) 1)}}{{template "participant-avatar-stack" dict "Names" .Timer.Participants "Small" true}}{{end}}
          <span class="label">{{if .MemberCount}}{{.MemberCount}}{{else}}0{{end}} members</span>
        </div>
      </div>

      <section class="grid two room-desk-grid">
        <div class="room-timer-column">
          {{if .Timer}}
          <article class="timer-card panel {{.Timer.Phase}}">
            {{if eq .Timer.Phase "lobby"}}
            <p class="label label-accent">Starting · join now</p>
            <div class="circle-timer running room-active-ring" role="timer" aria-label="Focus lobby countdown">
              <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
                <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
                <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" stroke-dashoffset="0"/>
              </svg>
              <div class="circle-timer-core">
                <div class="circle-timer-countdown countdown" aria-live="polite" data-seconds="{{timerSeconds .Timer}}" data-total="10">--:--</div>
              </div>
            </div>
            {{if not .Timer.Participant}}
            <form method="post" action="/r/{{.Room.Code}}/timer-join"><button type="submit" class="btn-primary">Join this block</button></form>
            {{else}}
            <form method="post" action="/r/{{.Room.Code}}/timer-leave"><button type="submit" class="btn-ghost timer-cancel">Leave this focus block</button></form>
            {{end}}
            {{else if eq .Timer.Phase "focus"}}
            <p class="label label-accent">Focus · session {{.Timer.CurrentSession}} of {{.Timer.TotalSessions}}</p>
            <div class="circle-timer running room-active-ring" role="timer">
              <svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">
                <circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>
                <circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" stroke-dashoffset="0"/>
              </svg>
              <div class="circle-timer-core">
                <div class="circle-timer-countdown countdown" aria-live="polite" data-seconds="{{secondsUntil .Timer.PhaseEndsAt}}" data-total="{{mul .Timer.FocusMinutes 60}}">--:--</div>
              </div>
            </div>
            {{if not .Timer.Participant}}
            <p class="label label-warn">Watching · join on next break</p>
            {{else}}
            <form method="post" action="/r/{{.Room.Code}}/timer-leave">
              <button type="submit" class="btn-ghost timer-cancel">Leave focus block</button>
            </form>
            {{end}}
            {{else}}
            <p class="label label-warn">{{if .Timer.PausedAt}}Break paused{{else}}Break · next block in{{end}} <span class="countdown mono" aria-live="polite" data-seconds="{{timerSeconds .Timer}}" data-paused="{{if .Timer.PausedAt}}true{{else}}false{{end}}">--:--</span></p>
            <div class="room-ready-time mono">{{.Timer.FocusMinutes}}:00</div>
            {{if not .Timer.Participant}}
            <form method="post" action="/r/{{.Room.Code}}/timer-join"><button type="submit" class="btn-primary">Join this block</button></form>
            {{end}}
            <form method="post" action="/r/{{.Room.Code}}/{{if .Timer.PausedAt}}timer-resume{{else}}timer-pause{{end}}">
              <button type="submit" class="{{if .Timer.PausedAt}}btn-primary{{else}}btn-ghost{{end}}">{{if .Timer.PausedAt}}Resume break{{else}}Pause break{{end}}</button>
            </form>
            <form method="post" action="/r/{{.Room.Code}}/timer-skip-break">
              <button type="submit" class="btn-ghost timer-cancel">Skip break &amp; continue</button>
            </form>
            {{if .Timer.Participant}}
            <form method="post" action="/r/{{.Room.Code}}/timer-leave"><button type="submit" class="btn-ghost timer-cancel">Leave this focus block</button></form>
            {{end}}
            {{end}}
            {{if gt (len .Timer.Participants) 0}}{{template "participant-avatar-stack" dict "Names" .Timer.Participants "Small" true}}{{end}}
            <p class="label">{{if eq .Timer.Phase "lobby"}}{{len .Timer.Participants}} joined{{else if eq (len .Timer.Participants) 1}}Focusing solo{{else}}{{len .Timer.Participants}} focusing{{end}}</p>
          </article>
          {{else}}
          <article class="timer-card panel idle ready-card">
            <p class="label label-accent">Ready · {{.Room.AutoSessions}} × {{.Room.FocusMinutes}}/{{.Room.BreakMinutes}}</p>
            <div class="room-ready-time mono">{{.Room.FocusMinutes}}:00</div>
            <form method="post" action="/r/{{.Room.Code}}/timer-start" data-room-name="{{.Room.Name}}"><button type="submit" class="btn-primary big-action">Start focus block</button></form>
            <p class="muted room-ready-hint">Start alone or with others — members get a join prompt when you start.</p>
          </article>
          <details class="room-details panel" data-room-section="settings">
            <summary><span class="label">Timer settings · {{.Room.AutoSessions}}×{{.Room.FocusMinutes}}/{{.Room.BreakMinutes}}</span><svg class="drawer-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg></summary>
            <form class="settings stack" method="post" action="/r/{{.Room.Code}}/settings">
              <label>Focus <input type="number" name="focus_minutes" min="5" max="180" value="{{.Room.FocusMinutes}}"></label>
              <label>Break <input type="number" name="break_minutes" min="1" max="60" value="{{.Room.BreakMinutes}}"></label>
              <label>Sessions <input type="number" name="auto_sessions" min="1" max="12" value="{{.Room.AutoSessions}}"></label>
              <button type="submit" class="btn-primary btn-compact">Save</button>
            </form>
          </details>
          <details class="room-details panel" data-room-section="share">
            <summary><span class="label">Share room</span><svg class="drawer-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg></summary>
            <p class="room-share room-share-block" data-room-path="/r/{{.Room.Code}}">
              <button type="button" class="copy-chip mono room-share-code">junkie.app/r/{{.Room.Code}}</button>
              <button type="button" class="btn-ghost btn-compact room-share-copy">Copy</button>
              <span class="room-share-feedback" aria-live="polite"></span>
            </p>
          </details>
          <details class="room-details panel" data-room-section="controls">
            <summary><span class="label">Room controls</span><svg class="drawer-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg></summary>
            <p class="muted">During focus, late joiners can watch the countdown but cannot enter the active block. During break, anyone can join the next block.</p>
            {{if eq .Room.CreatorID .User.ID}}
            <form method="post" action="/r/{{.Room.Code}}/delete" onsubmit="return confirm('Delete \'{{.Room.Name}}\'? This removes it for all {{.MemberCount}} members.')">
              <button type="submit" class="btn-danger">Delete room</button>
            </form>
            {{end}}
          </details>
          {{end}}
        </div>

        <article class="panel room-todos-panel">
          <div class="panel-title">
            <h2>Room todos</h2>
            <span class="label label-warn">Public to the room</span>
          </div>
          <form class="inline-form todo-add-form" method="post" action="/r/{{.Room.Code}}/todos" hx-post="/r/{{.Room.Code}}/todos" hx-target="#todo-groups-{{.Room.Code}}" hx-swap="outerHTML">
            <input name="text" placeholder="What are you working on?" required>
            <button type="submit" class="todo-add-plus" aria-label="Add task">+</button>
          </form>
          {{template "todo-groups-room" (.RoomTodosGrouped.View .Room.Code .User.DisplayName)}}
        </article>
      </section>
    </section>
    {{end}}
    <script>
      (function () {
        const code = '{{.Room.Code}}';
        const phaseKey = 'junkie:roomPhase:' + code;
        const card = document.querySelector('.timer-card');
        const phase = card?.classList.contains('break') ? 'break' : card?.classList.contains('focus') ? 'focus' : 'idle';
        const prev = sessionStorage.getItem(phaseKey);
        if (prev && prev !== phase) {
          if (prev === 'focus' && (phase === 'break' || phase === 'idle')) window.junkieNotify?.onTimerEnd('focus');
          else if (prev === 'break' && phase === 'focus') window.junkieNotify?.onTimerEnd('break');
        }
        sessionStorage.setItem(phaseKey, phase);

        document.querySelectorAll('.room-details').forEach((el) => {
          const section = el.dataset.roomSection;
          const key = 'junkie:room:' + code + ':open:' + section;
          if (localStorage.getItem(key) === '1') el.open = true;
          el.addEventListener('toggle', () => localStorage.setItem(key, el.open ? '1' : '0'));
        });

        const renameTrigger = document.querySelector('.room-rename-trigger');
        const renameForm = document.getElementById('room-rename-form');
        const nameDisplay = document.getElementById('room-name-display');
        renameTrigger?.addEventListener('click', () => {
          nameDisplay?.setAttribute('hidden', '');
          renameTrigger.setAttribute('hidden', '');
          renameForm?.removeAttribute('hidden');
          renameForm?.querySelector('input')?.focus();
        });
        document.querySelector('.room-rename-cancel')?.addEventListener('click', () => {
          renameForm?.setAttribute('hidden', '');
          nameDisplay?.removeAttribute('hidden');
          renameTrigger?.removeAttribute('hidden');
        });
        renameForm?.querySelector('input')?.addEventListener('keydown', (event) => {
          if (event.key === 'Escape') document.querySelector('.room-rename-cancel')?.click();
        });

        const ws = new WebSocket((location.protocol === 'https:' ? 'wss' : 'ws') + '://' + location.host + '/ws/r/' + code);
        ws.onmessage = (event) => window.junkieOnRoomWSMessage?.(code, event.data, '{{.Room.Name}}');
      })();
    </script>
  {{end}}
{{end}}
`

const appCSS = `
:root, [data-theme="light"] {
  color-scheme: light;
  --bg: #F2F0E6;
  --grid-line: rgba(28,35,30,0.05);
  --surface: #FBFAF3;
  --surface-2: #F2F0E6;
  --border: #E1DECD;
  --border-strong: #C9C5B2;
  --ink: #1D241F;
  --muted: #6F766A;
  --faint: #8A9083;
  --accent: #1E5C3C;
  --accent-btn: #1E5C3C;
  --accent-hover: #174A30;
  --accent-ink: #F4F6EF;
  --accent-soft: #EAF0E6;
  --accent-soft-border: #CFDCC9;
  --warn: #B3801F;
  --danger: #A8452F;
  --heat-0: #E7E5D8; --heat-1: #BFDCC6; --heat-2: #8CC3A0; --heat-3: #55A278; --heat-4: #2C7A52;
  --backdrop: rgba(9,12,9,0.55);
  --focus-ring: rgba(30,92,60,0.12);
  --radius-sm: 6px;
  --radius: 8px;
  --radius-lg: 12px;
  --radius-xl: 14px;
  --sp-1: 4px; --sp-2: 8px; --sp-3: 12px; --sp-4: 16px; --sp-5: 20px;
  --sp-6: 24px; --sp-8: 32px; --sp-10: 40px; --sp-12: 48px;
  --fs-display: 2.25rem;
  --fs-title: 1.625rem;
  --fs-card-title: 1.1875rem;
  --fs-body: 0.9rem;
  --fs-small: 0.8125rem;
  --fs-label: 0.65rem;
  --font-serif: "Source Serif 4", Georgia, "Times New Roman", serif;
  --font-mono: "IBM Plex Mono", ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  --font-sans: system-ui, -apple-system, "Segoe UI", Helvetica, Arial, sans-serif;
  --shadow-card: 0 1px 3px rgba(28,35,30,0.05);
  --shadow-auth: 0 2px 10px rgba(28,35,30,0.06);
  --shadow-drawer: -16px 0 48px rgba(28,35,30,0.18);
}
[data-theme="dark"] {
  color-scheme: dark;
  --bg: #121712;
  --grid-line: rgba(236,235,224,0.045);
  --surface: #1A211B;
  --surface-2: #121712;
  --border: #2A332B;
  --border-strong: #3A453B;
  --ink: #ECEBE0;
  --muted: #99A193;
  --faint: #6E7568;
  --accent: #7CC79A;
  --accent-btn: #2E7D53;
  --accent-hover: #35905F;
  --accent-ink: #F0F7F1;
  --accent-soft: #1E2A20;
  --accent-soft-border: #2E4A3A;
  --warn: #D8A84E;
  --danger: #C86A55;
  --heat-0: #202920; --heat-1: #274C36; --heat-2: #2F6B47; --heat-3: #3F8F60; --heat-4: #62BC85;
  --focus-ring: rgba(124,199,154,0.15);
  --shadow-card: none;
  --shadow-auth: none;
  --shadow-drawer: -16px 0 48px rgba(0,0,0,0.4);
}
* { box-sizing: border-box; }
html { font-size: 16px; }
body {
  margin: 0;
  min-height: 100vh;
  color: var(--ink);
  background-color: var(--bg);
  background-image:
    linear-gradient(var(--grid-line) 1px, transparent 1px),
    linear-gradient(90deg, var(--grid-line) 1px, transparent 1px);
  background-size: 36px 36px;
  font-family: var(--font-sans);
  font-size: var(--fs-body);
  transition: background-color 150ms ease, color 150ms ease;
}
body.focus-active {
  background-image: none;
}
body.focus-active .topbar {
  opacity: 0.35;
  transition: opacity 200ms ease;
}
body.focus-active .topbar:hover {
  opacity: 1;
}
.mono, .room-code-input, code {
  font-family: var(--font-mono);
}
.label {
  font-family: var(--font-mono);
  font-size: var(--fs-label);
  font-weight: 500;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--faint);
  margin: 0;
}
.label-accent { color: var(--accent); }
.label-warn { color: var(--warn); }
.mono-link {
  font-family: var(--font-mono);
  font-size: var(--fs-small);
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--accent);
  text-decoration: none;
}
.mono-link:hover { color: var(--accent-hover); }
a { color: var(--accent); text-decoration: none; }
a:hover { color: var(--accent-hover); }
button, input, select, textarea {
  font: inherit;
}
button, .btn {
  border: 0;
  border-radius: var(--radius);
  cursor: pointer;
  font-weight: 600;
  font-size: 0.9375rem;
}
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  line-height: 1;
  text-decoration: none;
}
button:focus-visible, input:focus-visible, summary:focus-visible, a:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.btn-primary, button[type="submit"]:not(.btn-ghost):not(.btn-danger):not(.check):not(.todo-action):not(.circle-timer-step):not(.theme-toggle):not(.menu-drawer-close):not(.menu-drawer-trigger):not(.desk-todos-mode):not(.todo-group-toggle):not(.todo-add-plus):not(.room-share-code):not(.room-share-copy-icon):not(.copy-chip):not(.room-rename-trigger):not(.focus-todos-pin):not(.banner-dismiss) {
  min-height: 48px;
  padding: 0 24px;
  border-radius: var(--radius);
  background: var(--accent-btn);
  color: var(--accent-ink);
}
.btn-primary:hover, button[type="submit"]:not(.btn-ghost):not(.btn-danger):not(.check):not(.todo-action):not(.circle-timer-step):hover {
  background: var(--accent-hover);
  color: var(--accent-ink);
}
.btn-compact { min-height: 44px; padding: 0 18px; font-size: 0.875rem; }
.btn-ghost, .ghost {
  min-height: 48px;
  padding: 0 24px;
  background: transparent;
  color: var(--muted);
  border: 1px solid var(--border);
}
.btn-ghost:hover, .ghost:hover {
  border-color: var(--border-strong);
  color: var(--ink);
  filter: none;
}
.btn-danger, .danger {
  min-height: 48px;
  padding: 0 24px;
  background: transparent;
  color: var(--danger);
  border: 1px solid color-mix(in srgb, var(--danger) 40%, transparent);
}
.btn-danger:hover, .danger:hover {
  background: var(--danger);
  color: var(--accent-ink);
  filter: none;
}
input, select, textarea {
  width: 100%;
  min-height: 44px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface-2);
  padding: 0 16px;
  color: var(--ink);
}
input::placeholder { color: var(--faint); }
input:focus {
  border-color: var(--accent);
  border-width: 1.5px;
  box-shadow: 0 0 0 3px var(--focus-ring);
  outline: none;
}
code {
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 0.1rem 0.35rem;
  background: var(--surface-2);
  font-family: var(--font-mono);
}
.topbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  min-height: 68px;
  padding: 0 clamp(20px, 4vw, 32px);
}
.topbar-actions {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
}
.theme-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 2rem;
  height: 2rem;
  padding: 0;
  background: transparent;
  color: var(--muted);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  min-height: 0;
}
.theme-toggle:hover { color: var(--ink); border-color: var(--border-strong); filter: none; }
.theme-toggle svg { width: 1rem; height: 1rem; }
[data-theme="light"] .theme-icon-dark, [data-theme="dark"] .theme-icon-light { display: none; }
.brand {
  display: inline-flex;
  align-items: center;
  gap: 0.65rem;
  color: var(--ink);
  text-decoration: none;
  font-family: var(--font-serif);
  font-weight: 600;
  font-size: 1.125rem;
  letter-spacing: -0.02em;
}
.brand-mark {
  display: grid;
  place-items: center;
  width: 2rem;
  height: 2rem;
  border-radius: 40% 60% 50% 50%;
  background: var(--accent-btn);
  color: var(--accent-ink);
  font-family: var(--font-serif);
  font-weight: 600;
}
.topbar-user {
  color: var(--muted);
  font-size: var(--fs-small);
  font-weight: 500;
}
.page {
  width: min(1400px, 100%);
  margin: 0 auto 4rem;
  padding: 0;
}
.page:has(.desk-shell) {
  max-width: none;
  margin-bottom: 0;
}
.context-banner {
  margin: var(--sp-3) clamp(20px, 4vw, 64px) 0;
  padding: var(--sp-3) var(--sp-4);
  background: var(--accent-soft);
  border: 1px solid var(--accent-soft-border);
  border-radius: var(--radius);
  color: var(--ink);
  font-size: var(--fs-small);
}
.context-banner-dismiss {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-3);
}
.banner-dismiss {
  background: transparent;
  color: var(--muted);
  border: 0;
  font-size: 1.25rem;
  line-height: 1;
  padding: 0;
  min-height: 0;
}
.auth-card, .panel, .timer-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
}
.auth-card {
  max-width: 420px;
  margin: 36px auto 0;
  padding: 32px 36px 36px;
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-auth);
}
.auth-card-top {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 2rem;
  margin-bottom: var(--sp-6);
}
.auth-back {
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 2rem;
  height: 2rem;
  color: var(--muted);
  text-decoration: none;
  border-radius: var(--radius);
}
.auth-back:hover { color: var(--ink); background: var(--surface-2); }
.auth-back svg { width: 1.25rem; height: 1.25rem; }
.auth-brand { display: flex; justify-content: center; }
.auth-title {
  font-family: var(--font-serif);
  font-size: var(--fs-title);
  font-weight: 600;
  line-height: 1.15;
  letter-spacing: -0.02em;
  margin: 0 0 var(--sp-4);
  text-align: center;
}
.auth-form label {
  display: flex;
  flex-direction: column;
  gap: var(--sp-2);
  font-weight: 600;
  font-size: var(--fs-small);
}
.auth-switch { margin: var(--sp-5) 0 0; text-align: center; font-size: var(--fs-small); }
.auth-switch a { font-weight: 600; }
.password-field { position: relative; display: block; }
.password-field input { padding-right: 44px; }
.password-toggle {
  position: absolute;
  right: 2px;
  top: 50%;
  transform: translateY(-50%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 2.25rem;
  height: 2.25rem;
  background: transparent;
  color: var(--muted);
}
.password-toggle:hover { color: var(--ink); }
.password-toggle svg { width: 1.15rem; height: 1.15rem; }
.password-toggle .icon-eye-off { display: none; }
.password-toggle.is-visible .icon-eye { display: none; }
.password-toggle.is-visible .icon-eye-off { display: block; }
h1, h2, p { margin-top: 0; }
h1, h2 {
  font-family: var(--font-serif);
  font-weight: 600;
}
h2 { font-size: var(--fs-card-title); letter-spacing: -0.02em; }
.eyebrow {
  font-family: var(--font-mono);
  font-size: var(--fs-label);
  font-weight: 500;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--accent);
}
.muted, .empty { color: var(--muted); font-size: var(--fs-small); }
.notice-error {
  color: var(--danger);
  background: color-mix(in srgb, var(--danger) 8%, var(--surface));
  border: 1px solid color-mix(in srgb, var(--danger) 25%, transparent);
  border-radius: var(--radius);
  padding: var(--sp-3) var(--sp-4);
  margin-bottom: var(--sp-4);
}
.notice-ok {
  color: var(--accent);
  background: color-mix(in srgb, var(--accent) 8%, var(--surface));
  border: 1px solid color-mix(in srgb, var(--accent) 25%, transparent);
  border-radius: var(--radius);
  padding: var(--sp-3) var(--sp-4);
  margin-bottom: var(--sp-4);
}
.stack { display: grid; gap: var(--sp-4); }
.panel { padding: 20px 22px; margin-bottom: var(--sp-4); }
.panel-title {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: var(--sp-4);
  border-bottom: 1px solid var(--border);
  margin-bottom: var(--sp-4);
  padding-bottom: var(--sp-3);
}
.panel-title h2 { margin: 0; font-size: var(--fs-card-title); }
.inline-form, .room-create, .room-join {
  display: flex;
  gap: var(--sp-2);
  align-items: stretch;
}
.inline-form.todo-add-form input,
.inline-form.todo-add-form .todo-add-plus { height: 44px; box-sizing: border-box; }
.inline-form input { flex: 1 1 auto; min-width: 0; }
.grid.two {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--sp-4);
}
.desk-shell {
  display: flex;
  flex-direction: column;
  min-height: calc(100vh - 68px);
}
.desk-shell:not(.desk-shell-focus) {
  max-height: calc(100vh - 68px);
  overflow: hidden;
}
.desk-shell-focus .focus-desk { flex: 1 1 auto; min-height: 0; }
.desk-room-bar {
  display: flex;
  justify-content: center;
  padding: 2px 0 var(--sp-3);
}
.desk-room-bar[hidden] { display: none !important; }
.room-membership-pill {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  padding: var(--sp-2) var(--sp-4);
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface);
  color: var(--muted);
  text-decoration: none;
  font-size: var(--fs-small);
}
.room-membership-pill:hover { border-color: var(--border-strong); }
.room-membership-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--accent);
  flex-shrink: 0;
}
.room-membership-label { color: var(--accent); }
.room-membership-name { color: var(--ink); font-weight: 500; }
.desk-grid {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: var(--sp-8);
  width: min(100%, 68rem);
  margin: 0 auto;
  padding: 20px clamp(20px, 5vw, 64px) 0;
  align-items: center;
  flex: 1 1 auto;
  min-height: 0;
}
.desk-ring-column {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--sp-4);
  width: 100%;
  min-width: 0;
}
.desk-timer-view { width: 100%; min-width: 0; }
.desk-timer-view[hidden] { display: none !important; }
.desk-timer-card,
.desk-todos-panel {
  width: 100%;
  height: min(32rem, calc(100vh - 10rem));
  margin-bottom: 0;
}
.desk-timer-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--sp-3);
  min-width: 0;
  overflow: hidden;
  text-align: center;
}
.desk-timer-card > .label { margin: 0; text-align: center; }
.desk-timer-card > form { margin: 0; }
.desk-timer-card .circle-timer.running,
.desk-timer-card .circle-timer.break-running {
  width: min(18rem, 100%);
  flex: 0 1 auto;
}
.desk-timer-footer {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--sp-3);
  width: 100%;
}
.desk-timer-footer form,
.desk-timer-footer p { margin: 0; }
.desk-timer-footer .timer-cancel { margin-top: 0; }
.desk-timer-footer .label { width: 100%; text-align: center; }
.desk-ring-hint { text-align: center; }
.desk-join-link {
  margin: var(--sp-4) 0 0;
  text-align: right;
}
.panel-title .desk-join-link { margin: 0; flex-shrink: 0; }
.desk-todos-panel {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-height: 0;
}
.desk-todos-panel .panel-title,
.desk-todos-panel .todo-add-form { flex-shrink: 0; }
.desk-todos-head { align-items: center; overflow: visible; min-width: 0; gap: var(--sp-3); }
.desk-todos-switch { position: relative; z-index: 2; flex: 1 1 auto; min-width: 0; }
.desk-todos-mode {
  display: inline-flex;
  align-items: center;
  gap: var(--sp-2);
  padding: var(--sp-2) var(--sp-3);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface-2);
  color: var(--ink);
  font-weight: 600;
  font-size: var(--fs-body);
  min-height: 44px;
  max-width: 100%;
  white-space: nowrap;
}
.desk-todos-mode-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}
.desk-todos-chevron { flex-shrink: 0; width: 0.9rem; height: 0.9rem; color: var(--muted); }
.desk-todos-menu {
  position: absolute;
  top: calc(100% + var(--sp-2));
  left: 0;
  z-index: 20;
  min-width: 14rem;
  padding: var(--sp-2);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
  box-shadow: var(--shadow-card);
  display: grid;
  gap: var(--sp-1);
}
.desk-todos-menu[hidden] { display: none !important; }
.desk-todos-menu button {
  display: block;
  width: 100%;
  padding: var(--sp-2) var(--sp-3);
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--ink);
  font-size: var(--fs-small);
  text-align: left;
  min-height: 0;
}
.desk-todos-menu button:hover { background: var(--surface-2); }
.desk-todos-menu button.is-active { background: var(--accent-soft); font-weight: 600; }
.desk-todos-menu button .mono { color: var(--faint); margin-left: var(--sp-2); }
.desk-todos-hint { flex-shrink: 0; }
.desk-todos-hint[hidden] { display: none !important; }
.desk-todos-view { display: flex; flex-direction: column; flex: 1 1 auto; min-height: 0; }
.desk-todos-view[hidden] { display: none !important; }
.desk-todos-panel .todo-groups,
.desk-todos-panel .todo-list { flex: 1 1 auto; min-height: 0; overflow-y: auto; }
.todo-list {
  list-style: none;
  margin: var(--sp-4) 0 0;
  padding: 0;
  display: grid;
  gap: var(--sp-2);
  align-content: start;
}
.todo-list li {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: var(--sp-3);
  min-height: 40px;
  height: auto;
  padding: var(--sp-1) var(--sp-2);
  border-radius: var(--radius-sm);
}
.todo-list li:hover:not(.empty) { background: var(--surface-2); }
.todo-list li.done > span:not(:has(.todo-author)) { color: var(--faint); text-decoration: line-through; }
.todo-list li.done .todo-text,
.todo-list li.done .todo-author { color: var(--faint); text-decoration: line-through; }
.todo-list li.done .todo-sep { color: var(--faint); }
.todo-list li > span {
  min-width: 0;
  overflow-wrap: anywhere;
}
.todo-list li.removed > span { color: var(--danger); }
.todo-list li > form:has(.check),
.todo-list li > .check {
  flex-shrink: 0;
  margin: 0;
}
.todo-list li > form:has(.check) {
  display: flex;
  align-items: flex-start;
}
.todo-list li.removed .todo-actions {
  display: flex;
  flex-shrink: 0;
  align-items: flex-start;
  gap: 0;
}
.todo-list li.removed .todo-actions form {
  display: flex;
  margin: 0;
}
.check {
  width: 18px;
  height: 18px;
  min-width: 18px;
  min-height: 18px;
  flex-shrink: 0;
  aspect-ratio: 1;
  padding: 0;
  border-radius: 50%;
  border: 1.5px solid var(--border-strong);
  background: transparent;
  color: var(--ink);
  font-size: 0.7rem;
  display: grid;
  place-items: center;
  box-sizing: border-box;
}
.todo-list li.done .check {
  background: var(--accent-btn);
  border-color: var(--accent-btn);
  color: var(--accent-ink);
}
.todo-avatar {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: var(--accent-btn);
  color: var(--accent-ink);
  font-family: var(--font-serif);
  font-size: 0.625rem;
  font-weight: 600;
  line-height: 1;
  display: grid;
  place-items: center;
  flex-shrink: 0;
}
.todo-sep { color: var(--faint); }
.todo-author {
  font-size: var(--fs-small);
  color: var(--faint);
  font-weight: 400;
}
.todo-action {
  background: transparent;
  color: var(--muted);
  padding: var(--sp-1);
  width: 32px;
  height: 32px;
  min-height: 0;
  border-radius: var(--radius-sm);
  opacity: 0;
}
.todo-list li:hover .todo-action, .todo-list li:focus-within .todo-action { opacity: 1; }
.todo-list li.removed .todo-action { opacity: 1; }
.todo-action svg { width: 1rem; height: 1rem; }
.todo-delete { color: var(--danger); }
.todo-restore { color: var(--accent); }
.todo-add-plus {
  flex: 0 0 auto;
  min-width: 44px;
  min-height: 44px;
  padding: 0;
  font-size: 1.2rem;
  line-height: 1;
  font-weight: 500;
  background: var(--accent-btn);
  color: var(--accent-ink);
  transition: background 120ms ease, color 120ms ease;
}
.todo-add-plus:disabled,
.todo-add-plus:disabled:hover {
  background: var(--surface-2);
  color: var(--faint);
  cursor: default;
}
.todo-groups { display: flex; flex-direction: column; gap: var(--sp-3); margin-top: var(--sp-3); }
.todo-group-toggle {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  width: 100%;
  padding: var(--sp-2);
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--faint);
  text-align: left;
  min-height: 0;
}
.todo-group-chevron { width: 0.85rem; height: 0.85rem; transition: transform 120ms ease; }
.todo-group.is-collapsed .todo-group-chevron { transform: rotate(-90deg); }
.todo-group.is-collapsed .todo-list { display: none; }
.circle-timer-wrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--sp-4);
  padding: var(--sp-6) 0;
}
.circle-timer {
  position: relative;
  aspect-ratio: 1;
  display: grid;
  place-items: center;
  cursor: pointer;
  background: transparent;
  padding: 0;
  color: inherit;
}
.circle-timer.idle { width: min(300px, 82vw); }
.circle-timer.running, .room-focus-ring, .room-active-ring { width: min(340px, 90vw); cursor: default; }
.circle-timer.idle:hover .circle-timer-progress { stroke: var(--accent-hover); }
.circle-timer-svg {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  transform: rotate(-90deg);
}
.circle-timer-track { stroke: var(--border); }
.circle-timer.idle .circle-timer-track,
.circle-timer.idle .circle-timer-progress { stroke-width: 7; }
.circle-timer.running .circle-timer-track,
.circle-timer.running .circle-timer-progress,
.room-focus-ring .circle-timer-track,
.room-focus-ring .circle-timer-progress,
.room-active-ring .circle-timer-track,
.room-active-ring .circle-timer-progress,
.circle-timer.break-offer .circle-timer-track,
.circle-timer.break-offer .circle-timer-progress,
.circle-timer.break-running .circle-timer-track,
.circle-timer.break-running .circle-timer-progress { stroke-width: 5; }
.circle-timer.break-offer,
.circle-timer.break-running { width: min(340px, 90vw); cursor: default; }
.circle-timer.break-offer .circle-timer-progress,
.circle-timer.break-running .circle-timer-progress { stroke: var(--warn); }
.circle-timer.breather { animation: breather 4s ease-in-out infinite; }
@keyframes breather {
  0%, 100% { transform: scale(1); opacity: 1; }
  50% { transform: scale(1.015); opacity: 0.94; }
}
.circle-timer-progress {
  stroke: var(--accent);
  stroke-linecap: round;
  transition: stroke-dashoffset 300ms linear, stroke 200ms ease;
}
.circle-timer-core {
  position: relative;
  z-index: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--sp-2);
  pointer-events: none;
}
.circle-timer-time { pointer-events: auto; }
.circle-timer-time input {
  width: 3.5ch;
  min-width: 3.5ch;
  min-height: 0;
  appearance: textfield;
  -moz-appearance: textfield;
  text-align: center;
  font-family: var(--font-mono);
  font-size: 3.75rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.03em;
  border: none;
  background: transparent;
  padding: 0;
  color: var(--ink);
  box-shadow: none;
}
.circle-timer-time input::-webkit-inner-spin-button,
.circle-timer-time input::-webkit-outer-spin-button {
  margin: 0;
  -webkit-appearance: none;
}
.circle-timer-time input:focus { box-shadow: none; border: none; }
.circle-timer-time.digits-3 input { font-size: 3rem; }
.circle-timer-countdown, .countdown {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-weight: 600;
  letter-spacing: -0.04em;
  line-height: 1;
}
.circle-timer-countdown { font-size: 5.125rem; }
.countdown { font-size: 4.875rem; }
.circle-timer-step {
  width: 36px;
  height: 36px;
  min-height: 0;
  padding: 0;
  border-radius: 50%;
  background: transparent;
  border: 1px solid var(--border);
  color: var(--muted);
  font-size: 1.1rem;
  pointer-events: auto;
}
.circle-timer-step:hover { border-color: var(--border-strong); color: var(--ink); filter: none; }
.circle-timer-step[hidden] { display: none; }
.circle-timer-step:disabled { opacity: 0.35; pointer-events: none; cursor: default; }
.join-prompt-backdrop {
  position: fixed;
  inset: 0;
  z-index: 40;
  display: grid;
  place-items: center;
  padding: var(--sp-5);
  background: var(--backdrop);
}
.join-prompt-card {
  width: min(100%, 24rem);
  text-align: center;
}
.join-prompt-card h2 {
  margin: var(--sp-3) 0 var(--sp-6);
  font-family: var(--font-serif);
  font-size: var(--fs-title);
  font-weight: 600;
}
.join-prompt-countdown {
  margin: calc(-1 * var(--sp-3)) 0 var(--sp-5);
  color: var(--accent);
  font-size: 1.5rem;
  font-weight: 600;
}
.join-prompt-actions {
  display: flex;
  gap: var(--sp-3);
  justify-content: center;
  flex-wrap: wrap;
}
.join-prompt-actions form { margin: 0; }
.timer-cancel { margin-top: var(--sp-4); }
.focus-desk {
  position: relative;
  min-height: calc(100vh - 68px);
  display: grid;
  place-items: center;
}
.focus-desk-main {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--sp-4);
}
.focus-todos-toggle {
  position: fixed;
  right: 0;
  top: 50%;
  transform: translateY(-50%);
  z-index: 21;
  padding: var(--sp-4) var(--sp-2);
  border: 1px solid var(--border);
  border-right: 0;
  border-radius: var(--radius) 0 0 var(--radius);
  background: var(--surface);
  color: var(--faint);
  font-family: var(--font-mono);
  font-size: var(--fs-label);
  font-weight: 500;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  writing-mode: vertical-rl;
  min-height: 0;
}
.focus-todos-toggle.peek-pulse { animation: peek-pulse 1.2s ease 2; }
@keyframes peek-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.45; }
}
.focus-todos-toggle.is-open { right: min(320px, 85vw); color: var(--accent); }
.focus-todos-backdrop {
  position: fixed;
  inset: 0;
  z-index: 19;
  background: var(--backdrop);
}
.focus-todos-backdrop[hidden] { display: none !important; }
.focus-todos-panel {
  position: fixed;
  top: 25vh;
  right: 0;
  height: 50vh;
  width: min(320px, 85vw);
  z-index: 20;
  display: flex;
  flex-direction: column;
  background: var(--surface);
  border: 1px solid var(--border);
  border-right: none;
  border-radius: var(--radius) 0 0 var(--radius);
  padding: var(--sp-5);
  transform: translateX(100%);
  transition: transform 200ms ease-out;
}
.focus-todos-panel.is-open { transform: translateX(0); }
.focus-todos-panel .panel-title { flex-shrink: 0; }
.focus-todos-panel .todo-list {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
}
.focus-todos-pin {
  position: absolute;
  top: var(--sp-3);
  right: var(--sp-3);
  width: 2rem;
  height: 2rem;
  min-height: 0;
  padding: 0;
  background: transparent;
  color: var(--muted);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
}
.focus-todos-pin.is-pinned { color: var(--accent); border-color: var(--accent); }
.focus-todos-pin svg { width: 0.9rem; height: 0.9rem; }
.menu-drawer-backdrop {
  position: fixed;
  inset: 0;
  z-index: 90;
  background: var(--backdrop);
}
.menu-drawer {
  position: fixed;
  top: 0;
  right: 0;
  z-index: 100;
  width: min(380px, 100%);
  height: 100vh;
  padding: var(--sp-5);
  overflow-y: auto;
  background: var(--surface);
  border-left: 1px solid var(--border);
  box-shadow: var(--shadow-drawer);
  transform: translateX(100%);
  transition: transform 200ms ease-out;
  display: flex;
  flex-direction: column;
  gap: var(--sp-4);
}
[data-theme="dark"] .menu-drawer { background: #161D17; }
.menu-drawer.open { transform: translateX(0); }
.menu-drawer-head {
  display: flex;
  justify-content: flex-end;
  align-items: center;
}
.menu-drawer-close {
  background: transparent;
  color: var(--muted);
  font-size: 1.5rem;
  line-height: 1;
  padding: var(--sp-1);
  min-height: 0;
  border: 0;
}
.drawer-identity { display: grid; gap: var(--sp-4); }
.drawer-identity-user {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
}
.drawer-avatar {
  width: 44px;
  height: 44px;
  border-radius: 50%;
  background: var(--accent-soft);
  color: var(--accent);
  display: grid;
  place-items: center;
  font-family: var(--font-serif);
  font-weight: 600;
  flex-shrink: 0;
}
.drawer-profile-row {
  display: flex;
  align-items: center;
  gap: var(--sp-3);
  color: var(--ink);
  text-decoration: none;
  font-weight: 600;
}
.drawer-name { font-family: var(--font-serif); font-size: 1.125rem; font-weight: 600; }
.drawer-profile-link { text-decoration: none; display: inline-block; margin-top: var(--sp-1); }
.drawer-guest-hint { font-size: var(--fs-label); }
.drawer-section-label { margin: 0; }
.drawer-signin { width: 100%; text-align: center; text-decoration: none; display: grid; place-items: center; }
.drawer-details {
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--surface);
  overflow: hidden;
}
.drawer-details summary, .room-details summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-3);
  padding: 14px 18px;
  cursor: pointer;
  list-style: none;
}
.drawer-details summary::-webkit-details-marker, .room-details summary::-webkit-details-marker { display: none; }
.drawer-chevron { width: 1rem; height: 1rem; flex-shrink: 0; transition: transform 120ms ease; color: var(--muted); }
.drawer-details[open] .drawer-chevron, .room-details[open] .drawer-chevron { transform: rotate(180deg); }
.drawer-details form, .room-details > :not(summary) { padding: 0 18px 18px; }
.room-list { display: grid; gap: var(--sp-2); }
.room-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-3);
  min-height: 44px;
  padding: var(--sp-3) var(--sp-4);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  color: var(--ink);
  text-decoration: none;
  background: transparent;
}
.room-row:hover { background: var(--surface-2); }
.room-row.is-here {
  background: var(--accent-soft);
  border-color: var(--accent-soft-border);
}
.room-row-main { display: grid; gap: 2px; min-width: 0; }
.room-row strong { font-weight: 600; font-size: var(--fs-body); }
.room-row-code { font-size: 0.75rem; color: var(--faint); }
.here-tag { color: var(--accent); flex-shrink: 0; }
.menu-drawer-logout { margin-top: auto; padding-top: var(--sp-4); border-top: 1px solid var(--border); }
.menu-drawer-logout button { width: 100%; }
.menu-drawer .room-create input, .menu-drawer .room-join input { min-height: 40px; }
.menu-drawer-trigger {
  display: grid;
  gap: 5px;
  width: 2.5rem;
  height: 2.5rem;
  padding: 0.55rem;
  background: transparent;
  color: var(--ink);
  border: none;
  min-height: 0;
}
.menu-bar { display: block; height: 2px; background: currentColor; border-radius: 1px; }
body.menu-drawer-open { overflow: hidden; }
.room-shell { padding: 0 clamp(20px, 4vw, 32px); }
.room-header-new {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: var(--sp-6);
  margin-bottom: var(--sp-8);
  padding-top: var(--sp-4);
}
.room-name-display, .room-focus-name {
  font-family: var(--font-serif);
  font-size: var(--fs-display);
  font-weight: 600;
  line-height: 1.1;
  letter-spacing: -0.03em;
  margin: var(--sp-2) 0 0;
}
.room-focus-name {
  font-size: 1.0625rem;
  font-style: italic;
  font-weight: 500;
  color: var(--muted);
}
.room-title-row { display: flex; align-items: center; gap: var(--sp-3); }
.room-rename-trigger {
  background: transparent;
  color: var(--muted);
  border: 1px solid var(--border);
  width: 2rem;
  height: 2rem;
  min-height: 0;
  padding: 0;
  display: grid;
  place-items: center;
}
.room-rename-form { display: flex; gap: var(--sp-2); margin-top: var(--sp-3); }
.room-eyebrow { display: flex; flex-wrap: nowrap; align-items: center; gap: var(--sp-2); }
.room-eyebrow-label { flex-shrink: 0; }
.room-share {
  display: inline-flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: var(--sp-2);
}
.room-share-block { display: flex; flex-wrap: wrap; gap: var(--sp-2); }
.room-desk-grid { grid-template-columns: 1fr 400px; gap: 48px; align-items: start; }
.room-timer-column { display: grid; gap: var(--sp-4); }
.ready-card { text-align: center; padding: var(--sp-8) var(--sp-6); }
.room-ready-time {
  font-family: var(--font-mono);
  font-size: 3.5rem;
  font-weight: 600;
  letter-spacing: -0.03em;
  margin: var(--sp-4) 0;
}
.room-ready-hint { font-size: var(--fs-small); margin-top: var(--sp-4); }
.room-details { margin: 0; }
.big-action { width: 100%; max-width: 280px; }
.room-focus-page {
  display: flex;
  flex-direction: column;
  min-height: calc(100vh - 68px);
}
.room-focus-page .desk-room-bar {
  flex-shrink: 0;
  padding-top: var(--sp-3);
}
.room-focus-page .room-focus-shell {
  flex: 1 1 auto;
  min-height: 0;
}
.room-focus-shell {
  min-height: calc(100vh - 68px);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--sp-4);
  padding: var(--sp-8) var(--sp-5);
  text-align: center;
}
.participant-avatars {
  display: flex;
  gap: var(--sp-2);
  justify-content: center;
  flex-wrap: wrap;
}
.participant-avatars-stack {
  flex-wrap: nowrap;
  gap: 0;
}
.participant-avatars-stack .participant-avatar {
  margin-left: -10px;
  border: 2px solid var(--bg);
  box-sizing: border-box;
  position: relative;
}
.participant-avatars-stack .participant-avatar:first-child { margin-left: 0; }
.participant-avatar-overflow {
  background: var(--surface-2);
  color: var(--muted);
}
.participant-avatar {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: var(--accent-soft);
  color: var(--accent);
  font-family: var(--font-mono);
  font-size: 0.75rem;
  font-weight: 600;
  display: grid;
  place-items: center;
}
.participant-avatars-sm .participant-avatar { width: 28px; height: 28px; font-size: 0.7rem; }
.room-members-meta { text-align: right; display: grid; gap: var(--sp-2); justify-items: end; }
.copy-chip, .room-share-code {
  font-family: var(--font-mono);
  font-size: var(--fs-small);
  text-transform: uppercase;
  background: transparent;
  border: 1px dashed var(--border-strong);
  border-radius: var(--radius);
  padding: var(--sp-1) var(--sp-3);
  color: var(--accent);
  cursor: pointer;
  min-height: 0;
}
.room-share.is-copied .copy-chip, .room-share.is-copied .room-share-code, .room-share.is-copied .room-share-copy-icon {
  border-color: var(--accent);
  color: var(--accent);
}
.room-share-copy-icon {
  background: transparent;
  color: var(--muted);
  border: 1px solid var(--border);
  width: 2rem;
  height: 2rem;
  min-height: 0;
  padding: 0;
  display: grid;
  place-items: center;
  border-radius: var(--radius);
  cursor: pointer;
}
.room-share-copy-icon svg { width: 1rem; height: 1rem; }
.room-share-feedback { font-size: var(--fs-small); font-weight: 600; color: var(--accent); }
.settings { display: grid; gap: var(--sp-3); }
.settings label { display: grid; gap: var(--sp-2); font-size: var(--fs-small); font-weight: 600; }
.profile-page { max-width: 720px; margin: var(--sp-8) auto 0; padding: var(--sp-6); }
.profile-page h1 { font-size: var(--fs-card-title); margin: 0; }
.profile-preference {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-5);
  margin-top: var(--sp-6);
  padding-top: var(--sp-5);
  border-top: 1px solid var(--border);
}
.profile-preference strong { display: block; margin-bottom: var(--sp-1); }
.profile-preference p { margin: 0; }
.toggle-control { flex: 0 0 auto; cursor: pointer; }
.toggle-control input { position: absolute; opacity: 0; width: 1px; height: 1px; }
.toggle-control span {
  display: block;
  width: 3rem;
  height: 1.65rem;
  padding: 3px;
  border: 1px solid var(--border-strong);
  border-radius: 999px;
  background: var(--surface-2);
  transition: background 150ms ease;
}
.toggle-control span::after {
  content: "";
  display: block;
  width: 1.05rem;
  height: 1.05rem;
  border-radius: 50%;
  background: var(--muted);
  transition: transform 150ms ease, background 150ms ease;
}
.toggle-control input:checked + span { background: var(--accent-soft); border-color: var(--accent); }
.toggle-control input:checked + span::after { transform: translateX(1.3rem); background: var(--accent); }
.toggle-control input:focus-visible + span { outline: 2px solid var(--accent); outline-offset: 2px; }
.profile-stats {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--sp-3);
  margin-bottom: var(--sp-4);
}
.profile-stats div {
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: var(--sp-4);
  background: var(--surface-2);
}
.profile-stats strong { font-family: var(--font-serif); font-size: 2rem; display: block; line-height: 1; }
.profile-stats span { color: var(--muted); font-size: var(--fs-label); text-transform: uppercase; letter-spacing: 0.1em; }
.profile-follow-stub { margin-top: var(--sp-4); font-size: var(--fs-small); }
.profile-password-form {
  display: flex;
  flex-direction: column;
  gap: var(--sp-3);
  max-width: 340px;
  margin-top: var(--sp-3);
}
.profile-password-form label {
  display: flex;
  flex-direction: column;
  gap: var(--sp-1);
  font-size: var(--fs-small);
  color: var(--muted);
}
.profile-password-form button { align-self: flex-start; }
.profile-danger strong { color: var(--danger); }
.profile-delete-form {
  display: flex;
  flex-direction: column;
  gap: var(--sp-3);
  max-width: 340px;
  margin-top: var(--sp-3);
}
.profile-delete-form label {
  display: flex;
  flex-direction: column;
  gap: var(--sp-1);
  font-size: var(--fs-small);
  color: var(--muted);
}
.profile-delete-form button { align-self: flex-start; }
.heatmap-chart { width: 100%; }
.heatmap-summary { color: var(--muted); font-size: var(--fs-small); margin: 0 0 var(--sp-3); }
.heatmap-layout { display: flex; gap: var(--sp-2); width: 100%; }
.heatmap-dow {
  display: grid;
  grid-template-rows: repeat(7, 1fr);
  gap: 3px;
  font-size: 0.65rem;
  color: var(--muted);
  padding-top: 1.15rem;
  width: 1.75rem;
  flex-shrink: 0;
}
.heatmap-dow span {
  display: flex;
  align-items: center;
  line-height: 1;
}
.heatmap-main { flex: 1; min-width: 0; }
.heatmap-months {
  display: grid;
  grid-template-columns: repeat(var(--weeks), minmax(0, 1fr));
  gap: 3px;
  font-size: 0.68rem;
  color: var(--muted);
  margin-bottom: 4px;
  min-height: 1rem;
}
.heatmap-month {
  grid-column: calc(var(--col) + 1);
}
.heatmap-wrap { overflow-x: auto; width: 100%; }
.heatmap {
  display: grid;
  grid-template-rows: repeat(7, minmax(0, 1fr));
  grid-auto-flow: column;
  grid-auto-columns: minmax(0, 1fr);
  gap: 3px;
  width: 100%;
  min-height: 108px;
}
.heatmap-legend {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  justify-content: flex-end;
  margin-top: var(--sp-3);
  font-size: 0.68rem;
  color: var(--muted);
}
.cell {
  aspect-ratio: 1;
  width: 100%;
  min-width: 8px;
  border-radius: 2.5px;
  background: var(--heat-0);
}
.cell-empty { visibility: hidden; }
.cell.l0 { background: var(--heat-0); }
.cell.l1 { background: var(--heat-1); }
.cell.l2 { background: var(--heat-2); }
.cell.l3 { background: var(--heat-3); }
.cell.l4 { background: var(--heat-4); }
.heatmap-legend .cell { width: 10px; height: 10px; flex-shrink: 0; }
.admin-shell { width: min(1180px, calc(100% - 40px)); margin: var(--sp-8) auto 0; }
.admin-header { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--sp-6); margin-bottom: var(--sp-6); }
.admin-header h1 { font-size: var(--fs-display); margin: var(--sp-1) 0 var(--sp-2); }
.admin-stats { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: var(--sp-4); }
.admin-stats .panel { display: grid; gap: var(--sp-3); }
.admin-stats strong { font-family: var(--font-serif); font-size: 2rem; line-height: 1; }
.admin-section { margin-top: var(--sp-6); }
.admin-section .panel-title > div { display: grid; gap: var(--sp-1); }
.admin-section .panel-title .eyebrow { margin: 0; }
.admin-table-wrap { width: 100%; overflow-x: auto; }
.admin-table { width: 100%; border-collapse: collapse; font-size: var(--fs-small); }
.admin-table th, .admin-table td { padding: var(--sp-3); border-bottom: 1px solid var(--border); text-align: left; vertical-align: middle; white-space: nowrap; }
.admin-table th { color: var(--faint); font-family: var(--font-mono); font-size: var(--fs-label); font-weight: 500; letter-spacing: 0.1em; text-transform: uppercase; }
.admin-table tbody tr:last-child td { border-bottom: 0; }
.admin-table form { margin: 0; }
.role-badge, .status-active { display: inline-flex; align-items: center; padding: 3px var(--sp-2); border-radius: 999px; font-family: var(--font-mono); font-size: var(--fs-label); text-transform: uppercase; letter-spacing: 0.08em; }
.role-badge { color: var(--muted); background: var(--surface-2); border: 1px solid var(--border); }
.role-admin, .status-active { color: var(--accent); background: var(--accent-soft); border: 1px solid var(--accent-soft-border); }
.role-owner { color: var(--warn); background: color-mix(in srgb, var(--warn) 9%, var(--surface)); border: 1px solid color-mix(in srgb, var(--warn) 25%, transparent); }
.forbidden-card { text-align: center; }
.forbidden-card .muted { margin-bottom: 0; }
.forbidden-card .btn { margin-top: var(--sp-5); }
@media (max-width: 720px) {
  .topbar { min-height: 60px; }
  .desk-grid, .room-desk-grid, .grid.two { grid-template-columns: 1fr; gap: var(--sp-6); padding-left: 20px; padding-right: 20px; }
  .desk-grid { width: 100%; }
  .desk-shell:not(.desk-shell-focus) {
    max-height: none;
    overflow: visible;
  }
  .page:has(.desk-shell) { margin-bottom: 4rem; }
  .desk-timer-card,
  .desk-todos-panel { height: auto; min-height: 28rem; }
  .desk-timer-card { overflow: visible; }
  .circle-timer.idle { width: min(250px, 88vw); }
  .circle-timer.running, .room-focus-ring { width: min(260px, 92vw); }
  .circle-timer-step { width: 44px; height: 44px; }
  .focus-todos-toggle {
    top: auto;
    bottom: 0;
    transform: none;
    writing-mode: horizontal-tb;
    border-right: 1px solid var(--border);
    border-radius: var(--radius) var(--radius) 0 0;
    width: auto;
    left: 50%;
    right: auto;
    translate: -50% 0;
  }
  .room-header-new { flex-direction: column; }
  .room-members-meta { text-align: left; justify-items: start; }
  .timer-cancel, .btn-ghost.timer-cancel { width: 100%; }
  .admin-header { flex-direction: column; }
  .admin-stats { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 480px) {
  .auth-card { margin: 20px 20px 0; padding: var(--sp-6); }
  .forbidden-card .btn { width: 100%; }
  .inline-form.todo-add-form { flex-direction: column; }
}
@media (prefers-reduced-motion: reduce) {
  *, body { transition: none !important; animation: none !important; }
  .menu-drawer, .focus-todos-panel, .focus-todos-toggle, .circle-timer-progress { transition: none !important; }
}
`
