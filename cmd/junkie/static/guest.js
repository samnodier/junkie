(function () {
  const deskRoot = document.getElementById('guest-desk');
  const profileMap = document.getElementById('guest-profile-work-map');
  if (!deskRoot && !profileMap) return;

  const keys = {
    todos: 'junkie:todos',
    timer: 'junkie:soloTimer',
    activity: 'junkie:activity',
  };

  const CIRC = 2 * Math.PI * 88;
  const clampMinutes = (n) => Math.min(180, Math.max(5, n));

  const breakMinutesForFocus = (focusMinutes) => {
    if (focusMinutes < 30) return 5;
    if (focusMinutes < 120) return 10;
    if (focusMinutes < 180) return 20;
    return 30;
  };

  const load = (key, fallback) => {
    try {
      return JSON.parse(localStorage.getItem(key)) ?? fallback;
    } catch {
      return fallback;
    }
  };

  const save = (key, value) => localStorage.setItem(key, JSON.stringify(value));

  const uid = () => Math.random().toString(36).slice(2, 10);

  const removeIconSVG =
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true">' +
      '<path d="M18 6 6 18M6 6l12 12"/>' +
    '</svg>';

  const deleteIconSVG =
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
      '<polyline points="3 6 5 6 21 6"/>' +
      '<path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>' +
    '</svg>';

  const restoreIconSVG =
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
      '<path d="M9 14 4 9l5-5"/>' +
      '<path d="M4 9h11a5 5 0 0 1 0 10h-3"/>' +
    '</svg>';

  const sortTodos = (todos) => {
    const active = [];
    const removed = [];
    for (const todo of todos) {
      (todo.removed ? removed : active).push(todo);
    }
    return active.concat(removed);
  };

  const todoRowClass = (todo) => {
    if (todo.removed) return 'removed';
    if (todo.done) return 'done';
    return '';
  };

  const heatLevel = (minutes) => {
    if (minutes >= 180) return 4;
    if (minutes >= 90) return 3;
    if (minutes >= 30) return 2;
    if (minutes > 0) return 1;
    return 0;
  };

  const dateKey = (date) => date.toISOString().slice(0, 10);

  const recordFocus = (minutes) => {
    const activity = load(keys.activity, {});
    const key = dateKey(new Date());
    activity[key] = (activity[key] || 0) + minutes;
    save(keys.activity, activity);
  };

  const normalizeTimer = () => {
    let timer = load(keys.timer, null);
    if (!timer) return null;

    if (!timer.phase) timer.phase = 'focus';

    if (timer.phase === 'focus') {
      const endsAt = new Date(timer.endsAt).getTime();
      if (Date.now() >= endsAt) {
        if (!timer.focusRecorded) {
          recordFocus(timer.focusMinutes);
          timer.focusRecorded = true;
        }
        timer.phase = 'break_offer';
        timer.breakMinutes = breakMinutesForFocus(timer.focusMinutes);
        delete timer.endsAt;
        save(keys.timer, timer);
      }
      return timer;
    }

    if (timer.phase === 'break_offer') return timer;

    if (timer.phase === 'break') {
      if (Date.now() >= new Date(timer.endsAt).getTime()) {
        save(keys.timer, null);
        return null;
      }
      return timer;
    }

    save(keys.timer, null);
    return null;
  };

  const secondsLeft = (timer) => {
    const left = Math.floor((new Date(timer.endsAt).getTime() - Date.now()) / 1000);
    return left > 0 ? left : 0;
  };

  const formatCountdown = (seconds) => {
    const m = String(Math.floor(seconds / 60)).padStart(2, '0');
    const s = String(seconds % 60).padStart(2, '0');
    return m + ':' + s;
  };

  const setRing = (timer, ratio) => {
    const ring = timer?.querySelector('.circle-timer-progress');
    if (!ring) return;
    const r = Math.max(0, Math.min(1, ratio));
    ring.style.strokeDashoffset = String(CIRC * (1 - r));
  };

  const ringSVG = () =>
    '<svg class="circle-timer-svg" viewBox="0 0 200 200" aria-hidden="true">' +
      '<circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none"/>' +
      '<circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-dasharray="553" stroke-dashoffset="0"/>' +
    '</svg>';

  const setFocusActive = (on) => {
    document.body.classList.toggle('focus-active', on);
  };

  const buildYearHeatmap = () => {
    const activity = load(keys.activity, {});
    const today = new Date();
    today.setHours(0, 0, 0, 0);

    const days = [];
    const dates = [];
    for (let i = 364; i >= 0; i--) {
      const d = new Date(today);
      d.setDate(today.getDate() - i);
      dates.push(d);
      const minutes = activity[dateKey(d)] || 0;
      days.push({
        date: d,
        label: d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' }),
        minutes,
        level: heatLevel(minutes),
      });
    }

    const firstDate = dates[0];
    const lastDate = dates[dates.length - 1];
    const gridStart = new Date(firstDate);
    gridStart.setDate(firstDate.getDate() - firstDate.getDay());

    let totalMinutes = 0;
    for (const day of days) {
      totalMinutes += day.minutes;
    }

    const dayByKey = {};
    for (const day of days) {
      dayByKey[dateKey(day.date)] = day;
    }

    const totalDays = Math.floor((lastDate - gridStart) / 86400000) + 1;
    const numWeeks = Math.ceil(totalDays / 7);
    const cells = Array.from({ length: numWeeks * 7 }, () => ({ empty: true }));

    for (let week = 0; week < numWeeks; week++) {
      for (let dow = 0; dow < 7; dow++) {
        const d = new Date(gridStart);
        d.setDate(gridStart.getDate() + week * 7 + dow);
        if (d < firstDate || d > lastDate) continue;
        const idx = week * 7 + dow;
        const key = dateKey(d);
        if (dayByKey[key]) {
          cells[idx] = dayByKey[key];
        } else {
          cells[idx] = {
            label: d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' }),
            minutes: 0,
            level: 0,
          };
        }
      }
    }

    const months = [];
    const labeled = {};
    for (let week = 0; week < numWeeks; week++) {
      for (let dow = 0; dow < 7; dow++) {
        const d = new Date(gridStart);
        d.setDate(gridStart.getDate() + week * 7 + dow);
        if (d < firstDate || d > lastDate) continue;
        if (d.getDate() === 1) {
          const key = d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0');
          if (!labeled[key]) {
            months.push({
              label: d.toLocaleDateString(undefined, { month: 'short' }),
              col: week,
            });
            labeled[key] = true;
          }
          break;
        }
      }
    }

    return { cells, months, weeks: numWeeks, totalMinutes };
  };

  const heatmapChartHTML = (showSummary = true) => {
    const { cells, months, weeks, totalMinutes } = buildYearHeatmap();
    const hours = Math.round(totalMinutes / 60);
    const monthHTML = months
      .map((m) => '<span class="heatmap-month" style="--col: ' + m.col + '">' + m.label + '</span>')
      .join('');
    const cellHTML = cells
      .map((day) =>
        day.empty
          ? '<span class="cell cell-empty"></span>'
          : '<span class="cell l' + day.level + '" title="' + day.label + ': ' + day.minutes + ' min"></span>'
      )
      .join('');

    return (
      '<div class="heatmap-chart">' +
        (showSummary ? '<p class="heatmap-summary">' + hours + ' hours focused in the last year</p>' : '') +
        '<div class="heatmap-layout">' +
          '<div class="heatmap-dow" aria-hidden="true">' +
            '<span></span><span>Mon</span><span></span><span>Wed</span><span></span><span>Fri</span><span></span>' +
          '</div>' +
          '<div class="heatmap-main">' +
            '<div class="heatmap-months" style="--weeks: ' + weeks + '">' + monthHTML + '</div>' +
            '<div class="heatmap-wrap">' +
              '<div class="heatmap" style="--weeks: ' + weeks + '" aria-label="Focus activity heat map">' +
                cellHTML +
              '</div>' +
            '</div>' +
          '</div>' +
        '</div>' +
        '<div class="heatmap-legend" aria-hidden="true">' +
          '<span>Less</span>' +
          '<span class="cell l0"></span>' +
          '<span class="cell l1"></span>' +
          '<span class="cell l2"></span>' +
          '<span class="cell l3"></span>' +
          '<span class="cell l4"></span>' +
          '<span>More</span>' +
        '</div>' +
      '</div>'
    );
  };

  const renderGuestProfile = () => {
    if (!profileMap) return;
    profileMap.innerHTML = heatmapChartHTML(true);
  };

  const focusTimerHTML = (timer) => {
    const total = timer.focusMinutes * 60;
    const left = secondsLeft(timer);
    return (
      '<p class="label label-accent">' + timer.focusMinutes + ' min focus</p>' +
      '<article class="circle-timer-wrap">' +
        '<div class="circle-timer running" role="timer" aria-label="Focus countdown">' +
          ringSVG() +
          '<div class="circle-timer-core">' +
            '<div class="circle-timer-countdown" id="guest-countdown" aria-live="polite" data-total="' + total + '">' +
              formatCountdown(left) +
            '</div>' +
          '</div>' +
        '</div>' +
        '<button type="button" class="btn-ghost timer-cancel" id="guest-timer-cancel">End early</button>' +
      '</article>'
    );
  };

  const breakOfferHTML = (timer) =>
    '<p class="label label-warn">' + timer.breakMinutes + ' min break</p>' +
    '<article class="circle-timer-wrap">' +
      '<div class="circle-timer break-offer breather" role="timer" aria-label="Break ready">' +
        ringSVG() +
        '<div class="circle-timer-core">' +
          '<div class="circle-timer-countdown" id="guest-countdown" aria-live="polite">' +
            formatCountdown(timer.breakMinutes * 60) +
          '</div>' +
        '</div>' +
      '</div>' +
      '<button type="button" class="btn-primary timer-cancel" id="guest-break-start">Start break</button>' +
      '<button type="button" class="btn-ghost timer-cancel" id="guest-break-skip">Skip break</button>' +
    '</article>';

  const breakRunningHTML = (timer) => {
    const total = timer.breakMinutes * 60;
    const left = secondsLeft(timer);
    return (
      '<p class="label label-warn">' + timer.breakMinutes + ' min break</p>' +
      '<article class="circle-timer-wrap">' +
        '<div class="circle-timer break-running breather" role="timer" aria-label="Break countdown">' +
          ringSVG() +
          '<div class="circle-timer-core">' +
            '<div class="circle-timer-countdown" id="guest-countdown" aria-live="polite" data-total="' + total + '">' +
              formatCountdown(left) +
            '</div>' +
          '</div>' +
        '</div>' +
        '<button type="button" class="btn-ghost timer-cancel" id="guest-break-skip">Skip break</button>' +
      '</article>'
    );
  };

  const idleTimerHTML = () =>
    '<div class="desk-ring-column">' +
    '<article class="circle-timer-wrap">' +
      '<form class="circle-timer-form" id="guest-timer-form">' +
        '<div class="circle-timer idle" role="group" aria-label="Set focus duration">' +
          ringSVG() +
          '<div class="circle-timer-core">' +
            '<button type="button" class="circle-timer-step" data-delta="-5" aria-label="Decrease 5 minutes">−</button>' +
            '<label class="circle-timer-time">' +
              '<input type="number" name="focus_minutes" min="5" max="180" value="50" aria-label="Focus minutes">' +
            '</label>' +
            '<button type="button" class="circle-timer-step" data-delta="5" aria-label="Increase 5 minutes">+</button>' +
          '</div>' +
        '</div>' +
      '</form>' +
    '</article>' +
    '<p class="label desk-ring-hint">Set minutes · tap ring to focus</p>' +
    '</div>';

  const wireIdleTimer = (form) => {
    const timer = form.querySelector('.circle-timer.idle');
    const input = form.querySelector('input[name="focus_minutes"]');
    if (!timer || !input) return;

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

    timer.querySelectorAll('.circle-timer-step').forEach((btn) => {
      btn.addEventListener('click', (event) => {
        event.preventDefault();
        event.stopPropagation();
        input.value = clampMinutes(Number(input.value) + Number(btn.dataset.delta));
        syncRing();
      });
    });

    input.addEventListener('click', (event) => event.stopPropagation());
    input.addEventListener('input', syncRing);
    input.addEventListener('change', syncRing);
    timer.addEventListener('click', () => form.requestSubmit());
    syncRing();
  };

  let tickHandle = null;
  let focusTodosPeekOpen = false;
  let focusTodosHideTimer = null;
  let focusTodosPinned = localStorage.getItem('junkie:todosPinned') === '1';
  let focusPeekWired = false;
  let focusPeekPulseDone = localStorage.getItem('junkie:peekSeen') === '1';

  const syncFocusPeekOpen = () => {
    const toggle = document.querySelector('.focus-todos-toggle');
    const panel = document.querySelector('.focus-todos-panel');
    const backdrop = document.querySelector('.focus-todos-backdrop');
    if (!toggle || !panel) return;
    toggle.classList.toggle('is-open', focusTodosPeekOpen);
    panel.classList.toggle('is-open', focusTodosPeekOpen);
    toggle.setAttribute('aria-expanded', focusTodosPeekOpen ? 'true' : 'false');
    panel.setAttribute('aria-hidden', focusTodosPeekOpen ? 'false' : 'true');
    if (focusTodosPeekOpen && !focusTodosPinned) backdrop?.removeAttribute('hidden');
    else backdrop?.setAttribute('hidden', '');
  };

  const closeFocusPeek = () => {
    focusTodosPeekOpen = false;
    if (focusTodosHideTimer) {
      clearTimeout(focusTodosHideTimer);
      focusTodosHideTimer = null;
    }
    syncFocusPeekOpen();
  };

  const dismissFocusPeek = () => {
    if (focusTodosPinned) return;
    closeFocusPeek();
  };

  const scheduleFocusPeekDismiss = () => {
    if (focusTodosPinned) return;
    if (focusTodosHideTimer) clearTimeout(focusTodosHideTimer);
    focusTodosHideTimer = setTimeout(dismissFocusPeek, 10000);
  };

  const openFocusPeek = () => {
    focusTodosPeekOpen = true;
    syncFocusPeekOpen();
    scheduleFocusPeekDismiss();
  };

  const focusTodoPeekItemsHTML = (todos) => {
    const active = sortTodos(todos).filter((todo) => !todo.removed);
    if (!active.length) return '<li class="empty">Nothing yet. Add one thing worth finishing.</li>';
    return active
      .map(
        (todo) =>
          '<li class="' +
          todoRowClass(todo) +
          '" data-id="' +
          todo.id +
          '">' +
          '<button type="button" class="check guest-toggle" aria-label="' +
          (todo.done ? 'Mark incomplete' : 'Mark complete') +
          '">' +
          (todo.done ? '✓' : '○') +
          '</button>' +
          '<span>' +
          todo.text +
          '</span>' +
          '</li>'
      )
      .join('');
  };

  const focusDeskHTML = (mainHTML, todos) =>
    '<section class="focus-desk">' +
      '<div class="focus-desk-main">' +
        mainHTML +
      '</div>' +
      '<div class="focus-todos-backdrop"' + (focusTodosPeekOpen && !focusTodosPinned ? '' : ' hidden') + '></div>' +
      '<button type="button" class="focus-todos-toggle' +
        (focusTodosPeekOpen ? ' is-open' : '') +
        '" aria-expanded="' +
        (focusTodosPeekOpen ? 'true' : 'false') +
        '" aria-controls="focus-todos-panel">Todos</button>' +
      '<aside class="focus-todos-panel' +
        (focusTodosPeekOpen ? ' is-open' : '') +
        '" id="focus-todos-panel" aria-hidden="' +
        (focusTodosPeekOpen ? 'false' : 'true') +
        '">' +
        '<button type="button" class="focus-todos-pin' + (focusTodosPinned ? ' is-pinned' : '') + '" aria-label="Pin todos panel" title="Pin panel"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M12 17v5M9 3h6l1 7h4l-5 6v5H9v-5L4 10h4z"/></svg></button>' +
        '<div class="panel-title"><h2>Todos</h2></div>' +
        '<ul class="todo-list" id="guest-focus-todos">' +
          focusTodoPeekItemsHTML(todos) +
        '</ul>' +
      '</aside>' +
    '</section>';

  const wireFocusTodosPeek = () => {
    if (!deskRoot || focusPeekWired) return;
    focusPeekWired = true;

    deskRoot.addEventListener('click', (event) => {
      if (!deskRoot.querySelector('.focus-desk')) return;

      const pinBtn = event.target.closest('.focus-todos-pin');
      if (pinBtn) {
        event.stopPropagation();
        focusTodosPinned = !focusTodosPinned;
        localStorage.setItem('junkie:todosPinned', focusTodosPinned ? '1' : '0');
        pinBtn.classList.toggle('is-pinned', focusTodosPinned);
        if (focusTodosPinned && focusTodosHideTimer) {
          clearTimeout(focusTodosHideTimer);
          focusTodosHideTimer = null;
        }
        syncFocusPeekOpen();
        return;
      }

      if (event.target.closest('.focus-todos-backdrop')) {
        dismissFocusPeek();
        return;
      }

      const toggle = event.target.closest('.focus-todos-toggle');
      if (!toggle) return;
      if (focusTodosPeekOpen) closeFocusPeek();
      else openFocusPeek();
    });
  };

  const focusTodoInput = () => {
    document.getElementById('guest-todo-form')?.querySelector('input[name="text"]')?.focus();
  };

  const addGuestTodo = (text) => {
    const trimmed = text.trim();
    if (!trimmed) return false;
    const next = load(keys.todos, []);
    next.unshift({ id: uid(), text: trimmed, done: false, removed: false });
    save(keys.todos, next);
    return true;
  };

  const clearFocusDeskTimers = () => {
    if (tickHandle) {
      clearInterval(tickHandle);
      tickHandle = null;
    }
  };

  const wireFocusDeskTodos = (todos) => {
    document.getElementById('guest-focus-todos')?.addEventListener('click', (event) => {
      if (!event.target.closest('.guest-toggle')) return;
      const item = event.target.closest('[data-id]');
      if (!item) return;
      const id = item.dataset.id;
      const next = load(keys.todos, []).map((todo) =>
        todo.id === id ? { ...todo, done: !todo.done } : todo
      );
      save(keys.todos, next);
      render();
    });

    if (!focusPeekPulseDone) {
      focusPeekPulseDone = true;
      localStorage.setItem('junkie:peekSeen', '1');
      const toggle = document.querySelector('.focus-todos-toggle');
      toggle?.classList.add('peek-pulse');
      setTimeout(() => toggle?.classList.remove('peek-pulse'), 2400);
    }
    if (focusTodosPeekOpen) scheduleFocusPeekDismiss();
  };

  const showFocusDesk = (mainHTML, todos) => {
    setFocusActive(true);
    deskRoot.innerHTML = '<div class="desk-shell desk-shell-focus">' + focusDeskHTML(mainHTML, todos) + '</div>';
    wireFocusDeskTodos(todos);
  };

  const render = () => {
    if (!deskRoot) return;
    const prevTimer = load(keys.timer, null);
    const timer = normalizeTimer();
    const todos = load(keys.todos, []);
    clearFocusDeskTimers();

    if (timer?.phase === 'focus') {
      showFocusDesk(focusTimerHTML(timer), todos);

      document.getElementById('guest-timer-cancel')?.addEventListener('click', () => {
        if (!confirm("End this focus session? It won't count toward your map.")) return;
        focusTodosPeekOpen = false;
        focusTodosPinned = localStorage.getItem('junkie:todosPinned') === '1';
        if (focusTodosHideTimer) {
          clearTimeout(focusTodosHideTimer);
          focusTodosHideTimer = null;
        }
        save(keys.timer, null);
        setFocusActive(false);
        render();
      });

      const running = deskRoot.querySelector('.circle-timer.running');
      const total = timer.focusMinutes * 60;
      setRing(running, secondsLeft(timer) / total);

      tickHandle = setInterval(() => {
        const hadTimer = load(keys.timer, null);
        const active = normalizeTimer();
        if (!active || active.phase !== 'focus') {
          if (hadTimer?.phase === 'focus') window.junkieNotify?.onTimerEnd('focus');
          render();
          return;
        }
        const box = document.getElementById('guest-countdown');
        const left = secondsLeft(active);
        if (box) {
          box.textContent = formatCountdown(left);
          setRing(running, left / total);
        }
      }, 1000);
      return;
    }

    if (timer?.phase === 'break_offer') {
      if (prevTimer?.phase === 'focus') window.junkieNotify?.onTimerEnd('focus');
      showFocusDesk(breakOfferHTML(timer), todos);

      const offer = deskRoot.querySelector('.circle-timer.break-offer');
      setRing(offer, 1);

      document.getElementById('guest-break-start')?.addEventListener('click', () => {
        const current = load(keys.timer, null);
        if (!current || current.phase !== 'break_offer') return;
        current.phase = 'break';
        current.endsAt = new Date(Date.now() + current.breakMinutes * 60 * 1000).toISOString();
        save(keys.timer, current);
        render();
      });

      document.getElementById('guest-break-skip')?.addEventListener('click', () => {
        save(keys.timer, null);
        setFocusActive(false);
        render();
      });
      return;
    }

    if (timer?.phase === 'break') {
      showFocusDesk(breakRunningHTML(timer), todos);

      document.getElementById('guest-break-skip')?.addEventListener('click', () => {
        save(keys.timer, null);
        setFocusActive(false);
        render();
      });

      const running = deskRoot.querySelector('.circle-timer.break-running');
      const total = timer.breakMinutes * 60;
      setRing(running, secondsLeft(timer) / total);

      tickHandle = setInterval(() => {
        const hadTimer = load(keys.timer, null);
        const active = normalizeTimer();
        if (!active) {
          if (hadTimer?.phase === 'break') window.junkieNotify?.onTimerEnd('break');
          render();
          return;
        }
        const box = document.getElementById('guest-countdown');
        const left = secondsLeft(active);
        if (box) {
          box.textContent = formatCountdown(left);
          setRing(running, left / total);
        }
      }, 1000);
      return;
    }

    const todoItems = todos.length
      ? sortTodos(todos).map((todo) => {
          const actionBtn = todo.removed
            ? '<button type="button" class="todo-action todo-restore guest-restore" title="Bring back" aria-label="Bring back">' + restoreIconSVG + '</button>' +
              '<button type="button" class="todo-action todo-delete guest-delete" title="Delete permanently" aria-label="Delete permanently">' + deleteIconSVG + '</button>'
            : '<button type="button" class="todo-action todo-remove guest-remove" title="Remove" aria-label="Remove">' + removeIconSVG + '</button>';
          return (
            '<li class="' + todoRowClass(todo) + '" data-id="' + todo.id + '">' +
              '<button type="button" class="check guest-toggle" aria-label="' + (todo.done ? 'Mark incomplete' : 'Mark complete') + '">' + (todo.done ? '✓' : '○') + '</button>' +
              '<span>' + todo.text + '</span>' +
              actionBtn +
            '</li>'
          );
        }).join('')
      : '<li class="empty">Nothing yet. Add one thing worth finishing.</li>';

    deskRoot.innerHTML =
      '<div class="desk-shell">' +
      '<section class="grid two desk-grid">' +
        idleTimerHTML() +
        '<article class="panel desk-todos-panel">' +
          '<div class="panel-title"><h2>Private todos</h2></div>' +
          '<form class="inline-form todo-add-form" id="guest-todo-form">' +
            '<input name="text" placeholder="What do you need to do?" required>' +
            '<button type="submit" class="todo-add-plus" aria-label="Add task">+</button>' +
          '</form>' +
          '<ul class="todo-list" id="guest-todos">' + todoItems + '</ul>' +
          '<p class="desk-join-link"><a href="#" data-open-join class="mono-link">Have a room code?</a></p>' +
        '</article>' +
      '</section></div>';

    setFocusActive(false);

    document.querySelector('[data-open-join]')?.addEventListener('click', (event) => {
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

    const todoForm = document.getElementById('guest-todo-form');
    const todoInput = todoForm?.querySelector('input[name="text"]');

    const submitGuestTodo = () => {
      if (!todoInput || !addGuestTodo(todoInput.value)) return;
      todoInput.value = '';
      render();
      focusTodoInput();
    };

    todoForm?.addEventListener('submit', (event) => {
      event.preventDefault();
      submitGuestTodo();
    });

    todoInput?.addEventListener('keydown', (event) => {
      if (event.key !== 'Enter') return;
      event.preventDefault();
      submitGuestTodo();
    });

    document.getElementById('guest-todos')?.addEventListener('click', (event) => {
      const item = event.target.closest('[data-id]');
      if (!item) return;
      const id = item.dataset.id;
      let next = load(keys.todos, []);
      if (event.target.closest('.guest-toggle')) {
        next = next.map((todo) => (todo.id === id ? { ...todo, done: !todo.done } : todo));
      } else if (event.target.closest('.guest-remove')) {
        next = next.map((todo) => (todo.id === id ? { ...todo, removed: true } : todo));
      } else if (event.target.closest('.guest-restore')) {
        next = next.map((todo) => (todo.id === id ? { ...todo, removed: false } : todo));
      } else if (event.target.closest('.guest-delete')) {
        next = next.filter((todo) => todo.id !== id);
      } else {
        return;
      }
      save(keys.todos, next);
      render();
    });

    const timerForm = document.getElementById('guest-timer-form');
    wireIdleTimer(timerForm);
    timerForm?.addEventListener('submit', (event) => {
      event.preventDefault();
      window.junkieNotify?.requestPermission();
      const minutes = Number(new FormData(event.target).get('focus_minutes')) || 50;
      const focusMinutes = clampMinutes(minutes);
      const endsAt = new Date(Date.now() + focusMinutes * 60 * 1000).toISOString();
      save(keys.timer, { phase: 'focus', endsAt, focusMinutes });
      render();
    });
  };

  renderGuestProfile();
  if (deskRoot) {
    wireFocusTodosPeek();
    render();
  }
})();
