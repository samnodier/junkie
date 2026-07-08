(function () {
  const root = document.getElementById('guest-desk');
  if (!root) return;

  const keys = {
    todos: 'junkie:todos',
    timer: 'junkie:soloTimer',
    activity: 'junkie:activity',
  };

  const CIRC = 2 * Math.PI * 88;
  const clampMinutes = (n) => Math.min(180, Math.max(5, n));

  const load = (key, fallback) => {
    try {
      return JSON.parse(localStorage.getItem(key)) ?? fallback;
    } catch {
      return fallback;
    }
  };

  const save = (key, value) => localStorage.setItem(key, JSON.stringify(value));

  const uid = () => Math.random().toString(36).slice(2, 10);

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

  const finishTimerIfNeeded = () => {
    const timer = load(keys.timer, null);
    if (!timer) return null;
    const endsAt = new Date(timer.endsAt).getTime();
    if (Date.now() >= endsAt) {
      recordFocus(timer.focusMinutes);
      save(keys.timer, null);
      return null;
    }
    return timer;
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
      '<circle class="circle-timer-track" cx="100" cy="100" r="88" fill="none" stroke-width="10"/>' +
      '<circle class="circle-timer-progress" cx="100" cy="100" r="88" fill="none" stroke-width="10" stroke-dasharray="553" stroke-dashoffset="0"/>' +
    '</svg>';

  const workMapPanelHTML = () =>
    '<article class="panel work-map-panel">' +
      '<div class="panel-title"><h2>Work map</h2><span>Focused minutes per day</span></div>' +
      heatmapChartHTML() +
    '</article>';

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

  const heatmapChartHTML = () => {
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
        '<p class="heatmap-summary">' + hours + ' hours focused in the last year</p>' +
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

  const runningTimerHTML = (timer) => {
    const total = timer.focusMinutes * 60;
    const left = secondsLeft(timer);
    return (
      '<article class="circle-timer-wrap">' +
        '<div class="circle-timer running" role="timer" aria-label="Focus countdown">' +
          ringSVG() +
          '<div class="circle-timer-core">' +
            '<div class="circle-timer-countdown" id="guest-countdown" data-total="' + total + '">' +
              formatCountdown(left) +
            '</div>' +
          '</div>' +
        '</div>' +
        '<button type="button" class="timer-cancel" id="guest-timer-cancel">Cancel focus</button>' +
      '</article>'
    );
  };

  const idleTimerHTML = () =>
    '<article class="circle-timer-wrap">' +
      '<form class="circle-timer-form" id="guest-timer-form">' +
        '<div class="circle-timer idle" role="group" aria-label="Set focus duration">' +
          ringSVG() +
          '<div class="circle-timer-core">' +
            '<button type="button" class="circle-timer-step" data-delta="-5" aria-label="Decrease 5 minutes">−</button>' +
            '<label class="circle-timer-time">' +
              '<input type="number" name="focus_minutes" min="5" max="180" value="50" aria-label="Focus minutes">' +
              '<span class="circle-timer-suffix">min</span>' +
            '</label>' +
            '<button type="button" class="circle-timer-step" data-delta="5" aria-label="Increase 5 minutes">+</button>' +
          '</div>' +
          '<span class="circle-timer-hint">Tap to start</span>' +
        '</div>' +
      '</form>' +
    '</article>';

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

  const focusTodoInput = () => {
    document.getElementById('guest-todo-form')?.querySelector('input[name="text"]')?.focus();
  };

  const addGuestTodo = (text) => {
    const trimmed = text.trim();
    if (!trimmed) return false;
    const next = load(keys.todos, []);
    next.unshift({ id: uid(), text: trimmed, done: false });
    save(keys.todos, next);
    return true;
  };

  const render = () => {
    const timer = finishTimerIfNeeded();
    const todos = load(keys.todos, []);
    const inFocus = Boolean(timer);

    if (tickHandle) {
      clearInterval(tickHandle);
      tickHandle = null;
    }

    if (inFocus) {
      root.innerHTML =
        runningTimerHTML(timer) +
        '<details class="work-map-collapsible">' +
          '<summary class="work-map-link">Work map</summary>' +
          workMapPanelHTML() +
        '</details>';

      document.getElementById('guest-timer-cancel')?.addEventListener('click', () => {
        save(keys.timer, null);
        render();
      });

      const running = root.querySelector('.circle-timer.running');
      const total = timer.focusMinutes * 60;
      setRing(running, secondsLeft(timer) / total);

      tickHandle = setInterval(() => {
        const active = finishTimerIfNeeded();
        if (!active) {
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
      ? todos.map((todo) =>
          '<li class="' + (todo.done ? 'done' : '') + '" data-id="' + todo.id + '">' +
            '<button type="button" class="check guest-toggle">' + (todo.done ? '✓' : '○') + '</button>' +
            '<span>' + todo.text + '</span>' +
            '<button type="button" class="ghost guest-delete">Delete</button>' +
          '</li>'
        ).join('')
      : '<li class="empty">Add a private task for today.</li>';

    root.innerHTML =
      '<section class="grid two desk-grid">' +
        idleTimerHTML() +
        '<article class="panel">' +
          '<div class="panel-title"><h2>Private todos</h2><span>Stored on this device</span></div>' +
          '<form class="inline-form" id="guest-todo-form">' +
            '<input name="text" placeholder="What do you need to do?" required>' +
            '<button type="submit" class="todo-add-plus" aria-label="Add task">+</button>' +
          '</form>' +
          '<ul class="todo-list" id="guest-todos">' + todoItems + '</ul>' +
        '</article>' +
      '</section>' +
      '<details class="work-map-collapsible">' +
        '<summary class="work-map-link">Work map</summary>' +
        workMapPanelHTML() +
      '</details>';

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
      if (event.target.classList.contains('guest-toggle')) {
        next = next.map((todo) => (todo.id === id ? { ...todo, done: !todo.done } : todo));
      } else if (event.target.classList.contains('guest-delete')) {
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
      const minutes = Number(new FormData(event.target).get('focus_minutes')) || 50;
      const focusMinutes = clampMinutes(minutes);
      const endsAt = new Date(Date.now() + focusMinutes * 60 * 1000).toISOString();
      save(keys.timer, { endsAt, focusMinutes });
      render();
    });
  };

  render();
})();
