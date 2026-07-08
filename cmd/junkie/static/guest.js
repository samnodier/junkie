(function () {
  const root = document.getElementById('guest-desk');
  if (!root) return;

  const keys = {
    todos: 'junkie:todos',
    timer: 'junkie:soloTimer',
    activity: 'junkie:activity',
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

  const heatmapDays = () => {
    const activity = load(keys.activity, {});
    const days = [];
    const today = new Date();
    for (let i = 83; i >= 0; i--) {
      const d = new Date(today);
      d.setDate(today.getDate() - i);
      const key = dateKey(d);
      const minutes = activity[key] || 0;
      days.push({
        label: d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' }),
        minutes,
        level: heatLevel(minutes),
      });
    }
    return days;
  };

  const heatmapHTML = () =>
    heatmapDays()
      .map((day) => '<span class="cell l' + day.level + '" title="' + day.label + ': ' + day.minutes + ' min"></span>')
      .join('');

  let tickHandle = null;

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
        '<article class="timer-card focus">' +
          '<p class="eyebrow">Solo focus · hidden-desk mode</p>' +
          '<div class="countdown" id="guest-countdown">' + formatCountdown(secondsLeft(timer)) + '</div>' +
          '<p class="muted">Private todos and rooms are hidden until this block ends.</p>' +
        '</article>' +
        '<article class="panel">' +
          '<div class="panel-title"><h2>Work map</h2><span>Focused minutes per day</span></div>' +
          '<div class="heatmap" aria-label="Activity heat map">' + heatmapHTML() + '</div>' +
        '</article>';

      tickHandle = setInterval(() => {
        const active = finishTimerIfNeeded();
        if (!active) {
          render();
          return;
        }
        const box = document.getElementById('guest-countdown');
        if (box) box.textContent = formatCountdown(secondsLeft(active));
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
      '<section class="grid two">' +
        '<article class="panel">' +
          '<div class="panel-title"><h2>Private todos</h2><span>Stored on this device</span></div>' +
          '<form class="inline-form" id="guest-todo-form">' +
            '<input name="text" placeholder="What do you need to do?" required>' +
            '<button type="submit">Add</button>' +
          '</form>' +
          '<ul class="todo-list" id="guest-todos">' + todoItems + '</ul>' +
        '</article>' +
        '<article class="panel">' +
          '<div class="panel-title"><h2>Rooms</h2><span>Accounts only for shared rooms</span></div>' +
          '<p class="muted">Create or join a room when you want to study with others. Solo mode does not need an account.</p>' +
          '<p><a href="/login">Log in</a> · <a href="/signup">Create account</a></p>' +
        '</article>' +
      '</section>' +
      '<article class="timer-card idle">' +
        '<p class="eyebrow">Solo timer</p>' +
        '<h2>Run a private focus block.</h2>' +
        '<form class="inline-form" id="guest-timer-form">' +
          '<input type="number" name="focus_minutes" min="5" max="180" value="50" aria-label="Focus minutes">' +
          '<button type="submit">Start solo focus</button>' +
        '</form>' +
      '</article>' +
      '<article class="panel">' +
        '<div class="panel-title"><h2>Work map</h2><span>Focused minutes per day</span></div>' +
        '<div class="heatmap" aria-label="Activity heat map">' + heatmapHTML() + '</div>' +
      '</article>';

    const todoForm = document.getElementById('guest-todo-form');
    todoForm?.addEventListener('submit', (event) => {
      event.preventDefault();
      const input = todoForm.querySelector('input[name="text"]');
      const text = input.value.trim();
      if (!text) return;
      const next = load(keys.todos, []);
      next.unshift({ id: uid(), text, done: false });
      save(keys.todos, next);
      input.value = '';
      render();
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

    document.getElementById('guest-timer-form')?.addEventListener('submit', (event) => {
      event.preventDefault();
      const minutes = Number(new FormData(event.target).get('focus_minutes')) || 50;
      const focusMinutes = Math.min(180, Math.max(5, minutes));
      const endsAt = new Date(Date.now() + focusMinutes * 60 * 1000).toISOString();
      save(keys.timer, { endsAt, focusMinutes });
      render();
    });
  };

  render();
})();
