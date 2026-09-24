<script setup>
// Public documentation at /docs. The README is the developer's door — what
// junkie is, how to run it, how to deploy it. This is the user's: what every
// screen does, for people who will never clone the repository.
//
// One section on screen at a time, chosen from the rail, the way a
// documentation site works — not one endless scroll. The rail is then a place
// you go rather than a place you scroll past, which is why it stays put.
//
// Every section stays in the DOM (v-show, not v-if): the filter searches the
// text of all of them, and it can only do that if the text is there to read.
import { computed, nextTick, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import AppShell from '@/components/AppShell.vue';

const sections = [
  { id: 'start', title: 'Start here' },
  { id: 'guest', title: 'Without an account' },
  { id: 'accounts', title: 'Accounts' },
  { id: 'solo', title: 'Solo focus' },
  { id: 'rooms', title: 'Rooms' },
  { id: 'room-people', title: 'Members, admins, ownership' },
  { id: 'room-timers', title: 'Running a block' },
  { id: 'temporary', title: 'Temporary rooms' },
  { id: 'streaming', title: 'Streaming and overlays' },
  { id: 'sound', title: 'Chimes and room sounds' },
  { id: 'connections', title: 'Connections and profiles' },
  { id: 'phone', title: 'On your phone' },
  { id: 'terminal', title: 'Terminal client' },
  { id: 'discord', title: 'Discord bot' },
  { id: 'api', title: 'Adding todos by API' },
  { id: 'data', title: 'Where your data lives' },
  { id: 'admin', title: 'Admin and owner access' },
  { id: 'selfhost', title: 'Self-hosting' },
];
const ids = new Set(sections.map((s) => s.id));

const route = useRoute();
const router = useRouter();

// The hash is the state, so a deep link, the back button and a click all
// arrive the same way. Anything unrecognised falls back to the first section
// rather than showing nothing.
const active = computed(() => {
  const id = String(route.hash || '').slice(1);
  return ids.has(id) ? id : sections[0].id;
});

const filter = ref('');
const query = computed(() => filter.value.trim().toLowerCase());
// Searched off the rendered text, so a section is found by anything it says,
// not only by its title.
const matches = ref(null);
watch(query, async (q) => {
  if (!q) {
    matches.value = null;
    return;
  }
  await nextTick();
  const hits = new Set();
  for (const el of document.querySelectorAll('[data-docs-section]')) {
    if (el.textContent.toLowerCase().includes(q)) hits.add(el.dataset.docsSection);
  }
  matches.value = hits;
  // Narrowing the list should land you on a result rather than leaving you
  // on a section the filter just said is not one.
  if (hits.size > 0 && !hits.has(active.value)) {
    go(sections.find((s) => hits.has(s.id)).id);
  }
});

const listed = computed(() =>
  matches.value ? sections.filter((s) => matches.value.has(s.id)) : sections
);

const tocOpen = ref(false);
function go(id) {
  tocOpen.value = false;
  if (route.hash === `#${id}`) return;
  // push, not replace, so the back button walks back through the sections
  // someone read — the thing that makes this feel like pages.
  router.push({ hash: `#${id}` });
}

// A new section starts at its own beginning. Skipped on the very first
// render, where there is nothing to scroll back from.
let settled = false;
watch(active, async () => {
  await nextTick();
  if (!settled) {
    settled = true;
    return;
  }
  document.querySelector('.docs-content')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
}, { immediate: true });
</script>

<template>
  <AppShell>
    <div class="docs-page">
      <header class="docs-hero">
        <p class="label label-accent">Documentation</p>
        <h1>Everything junkie does</h1>
        <p class="docs-lede">
          junkie is a shared focus timer for study groups: run blocks alone or with other
          people, keep a todo list beside them, and watch the focused minutes stack up on a
          work map. It runs in a browser, on your phone, in a terminal and from Discord —
          the same rooms and the same account in all four.
        </p>
        <p class="muted docs-hero-meta">
          Building or self-hosting it instead?
          <a href="https://github.com/samnodier/junkie" target="_blank" rel="noopener">The README</a>
          covers the code.
        </p>
      </header>

      <div class="docs-body">
        <!-- Sidebar: a rail on wide screens, a disclosure on narrow ones. -->
        <nav class="docs-nav" :class="{ 'is-open': tocOpen }" aria-label="Documentation sections">
          <button type="button" class="docs-nav-toggle" @click="tocOpen = !tocOpen">
            <span>Contents</span>
            <svg class="drawer-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M6 9l6 6 6-6" /></svg>
          </button>
          <div class="docs-nav-inner">
            <label class="docs-search">
              <span class="visually-hidden">Filter the documentation</span>
              <input v-model="filter" type="search" placeholder="Filter…" autocomplete="off">
            </label>
            <p v-if="query" class="label docs-nav-count">
              {{ listed.length }} of {{ sections.length }} sections
            </p>
            <ul class="docs-nav-list">
              <li v-for="s in listed" :key="s.id">
                <button
                  type="button"
                  class="docs-nav-link"
                  :class="{ 'is-active': active === s.id }"
                  @click="go(s.id)"
                >{{ s.title }}</button>
              </li>
            </ul>
          </div>
        </nav>

        <div class="docs-content">
          <p v-if="query && listed.length === 0" class="panel docs-empty">
            Nothing here matches “{{ filter }}”. Try a shorter word — the filter matches the
            text of a whole section.
          </p>

          <section v-show="active === 'start'" id="start" data-docs-section="start" class="docs-section">
            <h2>Start here</h2>
            <p>
              A <strong>block</strong> is one run of the timer: a focus session, then a break,
              then the next session, for as many sessions as you set. Everything else in junkie
              hangs off that idea.
            </p>
            <p>
              You can run a block by yourself or share one with other people in a
              <strong>room</strong>. Every minute of focus you complete lands on your work map,
              a heatmap of focused days on your profile.
            </p>
            <div class="docs-callout">
              <p>
                <strong>You do not need an account to try it.</strong> Open junkie and the solo
                timer and a private todo list are there straight away — they just live in that
                browser rather than on the server. See <a href="#guest" @click.prevent="go('guest')">Without an account</a>.
              </p>
            </div>
          </section>

          <section v-show="active === 'guest'" id="guest" data-docs-section="guest" class="docs-section">
            <h2>Without an account</h2>
            <p>
              Guest mode keeps everything in that browser's <code>localStorage</code>, on that
              device only. Nothing reaches the server.
            </p>
            <ul>
              <li><strong>Private todos</strong> — add, complete, remove and restore. Completed and removed ones are deleted after 24 hours.</li>
              <li><strong>Solo timer</strong> — focus blocks with optional breaks.</li>
              <li><strong>Work map</strong> — focused minutes per day, stored locally.</li>
            </ul>
            <p>
              Guest data is tied to one browser profile on one device. Clearing site data or
              switching browsers starts you over, and <strong>guest data is never copied into an
              account</strong> when you sign up later. Sign up first if you want to keep it.
            </p>
            <p class="muted">
              The terminal client has its own guest mode with the same rule — see
              <a href="#terminal" @click.prevent="go('terminal')">Terminal client</a>.
            </p>
          </section>

          <section v-show="active === 'accounts'" id="accounts" data-docs-section="accounts" class="docs-section">
            <h2>Accounts</h2>
            <p>
              Create one when you want shared rooms, or data that follows you between devices.
              A username and a password is all it takes — junkie never collects an email address.
            </p>
            <ul>
              <li><strong>Usernames</strong> are 2–32 characters: lowercase letters, numbers, dots, dashes, underscores. Change yours at Profile → Account.</li>
              <li><strong>Changing your password</strong> (Profile → Security) signs out every other device, including the terminal client.</li>
              <li><strong>Profile picture</strong> — upload or remove at Profile → Preferences.</li>
              <li><strong>Deleting your account</strong> (Profile → Danger zone) permanently removes it along with the rooms you created, your todos and your activity. It asks for your password.</li>
            </ul>
            <h3>Forgotten passwords go through Discord</h3>
            <p>
              With no email address on file, there is nowhere to send a reset link — so junkie
              sends it over Discord instead.
              <a href="https://discord.gg/qEEzdXQHtK" target="_blank" rel="noopener">Join the junkie Discord server</a>
              and run <code>/junkie reset-password</code> in <code>#junkie-bot</code>. The bot DMs
              you a single-use link that expires in 30 minutes, limited to two requests per account
              per day.
            </p>
            <div class="docs-callout docs-callout-warn">
              <p>
                <strong>Link your Discord before you need it.</strong> Linking requires being
                signed in, so it cannot be done once the password is already forgotten. If you
                cannot use Discord at all, ask in <code>#junkie-bot</code> or open a
                <a href="https://github.com/samnodier/junkie/issues" target="_blank" rel="noopener">GitHub issue</a>
                — the owner can issue the same link by hand.
              </p>
            </div>
          </section>

          <section v-show="active === 'solo'" id="solo" data-docs-section="solo" class="docs-section">
            <h2>Solo focus</h2>
            <p>The ring on the desk is adjustable before you start:</p>
            <ul>
              <li>Scroll the ring, or use the arrow keys, to change by <strong>±1 minute</strong>.</li>
              <li>The <strong>±5</strong> buttons take larger steps.</li>
              <li>Tap the ring, or press Enter or Space, to start.</li>
            </ul>
            <p>
              When a focus block ends junkie offers the break. Take it, adjust its length on the
              ring, or hit <strong>Skip break &amp; continue</strong> to roll straight into another
              focus session of the same length.
            </p>
            <p>
              Signed in, runs and completed minutes are stored server-side and show up on your work
              map from any device. Private todo changes sync live between your tabs; a solo timer
              phase change reloads the desk in other tabs.
            </p>
          </section>

          <section v-show="active === 'rooms'" id="rooms" data-docs-section="rooms" class="docs-section">
            <h2>Rooms</h2>
            <p>
              A room is a persistent shared space at <code>/r/{code}</code> with its own members,
              todo board and timer. You need an account to create or join one.
            </p>
            <ul>
              <li>Create one from the menu, or join with a room code or an invite link. Invite links land on a confirmation screen first.</li>
              <li>Each account can own up to <strong>5 rooms</strong>; deleting one frees the slot. A room holds up to <strong>100 members</strong>.</li>
              <li>When someone starts a lobby while junkie is in the background, you can get a room invite notification — toggle it at Profile → Preferences.</li>
            </ul>

            <h3>The todo board</h3>
            <ul>
              <li>Room todos are <strong>public to every member</strong>, grouped into yours and everyone else's.</li>
              <li>You can complete your own; other people's are read-only, shown with their name and picture.</li>
              <li>Completed and removed todos are deleted after 24 hours, so the board stays about current work.</li>
              <li>Lists update live for everyone, and completing something raises a short toast for the rest of the room.</li>
            </ul>
            <p class="muted">
              On the desk you can switch between private todos and each room's board. Inside
              <code>/r/{code}</code> there is no switcher — that page is scoped to one room.
            </p>

            <h3>Room settings</h3>
            <p>
              Focus length, break length, session count, auto-start breaks and session check-in
              are <strong>open to every member</strong>, so a room can organise itself when nobody
              in particular is around. They are locked while a block is running, because a run
              takes a copy of its settings when it starts — with one deliberate exception, covered
              under <a href="#temporary" @click.prevent="go('temporary')">Temporary rooms</a>.
            </p>
          </section>

          <section v-show="active === 'room-people'" id="room-people" data-docs-section="room-people" class="docs-section">
            <h2>Members, admins, ownership</h2>
            <p>
              Every room has a <strong>creator</strong>, and can have any number of
              <strong>room admins</strong>. This is separate from the site-wide account roles under
              <a href="#admin" @click.prevent="go('admin')">Admin and owner access</a>.
            </p>
            <table class="docs-table">
              <thead><tr><th>Who</th><th>Can</th></tr></thead>
              <tbody>
                <tr><td>Any member</td><td>Timer settings, renaming, todos, starting and running blocks</td></tr>
                <tr><td>Room admin</td><td>The above, plus removing members and changing the room's sound</td></tr>
                <tr><td>Creator</td><td>The above, plus promoting and demoting admins, transferring the room, and deleting it</td></tr>
              </tbody>
            </table>
            <p>
              The roster lives at <code>/r/{code}/members</code> — creator first, then admins, then
              everyone else alphabetically, so the people who can act on the room read together at
              the top.
            </p>
            <h3>Handing a room over</h3>
            <p>
              A creator can transfer the room to another member. The transfer waits
              <strong>10 minutes</strong> before it takes effect and can be cancelled during that
              window, so a misclick is not permanent. Temporary rooms cannot change hands — they
              do not live long enough for it to mean anything.
            </p>
            <h3>The event log</h3>
            <p>
              <code>/r/{code}/history</code> records what happened to the room rather than in it:
              members joining, leaving and being removed, admins promoted and demoted, ownership
              transfers started, cancelled and completed, settings changed, the sound changed.
              Entries are kept for <strong>90 days</strong>.
            </p>
          </section>

          <section v-show="active === 'room-timers'" id="room-timers" data-docs-section="room-timers" class="docs-section">
            <h2>Running a block</h2>
            <p>
              Shared blocks run from server timestamps and count down locally, so everyone's ring
              agrees. A run goes:
            </p>
            <p class="docs-flow mono">lobby → focus → break → focus → … → ends</p>
            <p>
              The <strong>lobby</strong> is a 30-second join window showing who has arrived so far.
              Then focus, then a break, for as many sessions as the room is set to. There is no
              trailing break: the run ends when the last focus block does.
            </p>
            <ul>
              <li><strong>Joining late</strong> — during focus you are locked out until the next break. Tap Join and you are parked to board automatically at that break. Parking lapses after an hour and clears when its run ends, so a fresh block starts with the people who actually showed up.</li>
              <li><strong>Pause / Resume</strong> and <strong>Skip break</strong> are available to any member during a break, from the web or from the Discord message.</li>
              <li><strong>Leave focus block</strong> ends your participation. Closing a tab does not — the rings show who deliberately joined, not who has a tab open.</li>
              <li><strong>Focus mode</strong> hides the todo board during a session so the timer stays central.</li>
            </ul>

            <h3>Auto-start breaks</h3>
            <p>
              <strong>On</strong>: breaks begin by themselves and the whole block is hands-free.
              <strong>Off</strong>: when focus ends the break waits on an adjustable ring until
              somebody starts it — which is also your chance to change its length for that one break.
            </p>

            <h3>Session check-in</h3>
            <p>
              With check-in on, everyone must tap <strong>“I'm here”</strong> during each break to
              keep their seat in the next session; no-shows are dropped when focus starts and can
              rejoin at a later break. Joining, starting the run, or acting on the break all count
              as your check-in. Nobody is exempt, including whoever started the run. Skip break is
              disabled so the window cannot be cut short, and if nobody checks in, the run ends.
            </p>

            <div class="docs-callout">
              <p>
                <strong>Abandoned blocks clean themselves up.</strong> A break left paused for an
                hour — including one nobody ever started — ends its run, freeing the room for a
                fresh session. Nothing is lost: completed focus sessions were credited as they
                finished.
              </p>
            </div>
          </section>

          <section v-show="active === 'temporary'" id="temporary" data-docs-section="temporary" class="docs-section">
            <h2>Temporary rooms</h2>
            <p>
              For a quick block with a few people when you don't want a room that sticks around.
              <strong>Create temporary room</strong> in the menu opens a popup — focus, break and
              session counts, auto-run breaks, session check-in, and optionally a sound — and drops
              you on a stripped-down screen at <code>/f/{code}</code> with a share link at the top.
            </p>
            <ul>
              <li><strong>Joined by link</strong> — anyone signed in who opens it is added and queued, with no confirmation step. They land on the waiting screen, so you can see who is here before starting.</li>
              <li><strong>Just the timer</strong> — no todo board, no background grid. The invite link shows while you gather and during breaks, and disappears once focus starts.</li>
              <li><strong>Joinable mid-block</strong> — unlike a normal room, a temporary one lets people in during focus rather than making them wait for the break. Sharing it while it runs is the whole point.</li>
              <li><strong>Disposable</strong> — it deletes itself the moment the run finishes or everyone leaves, and sends everyone home. Focus minutes still count toward each person's work map.</li>
              <li>They do not count toward your 5-room limit, never appear in your room list, and ones created but never started are cleaned up automatically.</li>
            </ul>

            <h3>Changing the session count mid-block</h3>
            <p>
              A temporary room is the one place the number of sessions can move while the block is
              running. On any break, a stepper under the countdown reads
              <span class="docs-inline-ui">− 3 done of 6 +</span> — step it down to 4 when you
              realise four is all you have, or up when it is going well.
            </p>
            <ul>
              <li>The change lands about half a second after your last tap, so walking 6 down to 4 is one change rather than two.</li>
              <li>Anything reading the block follows automatically, including the <a href="#streaming" @click.prevent="go('streaming')">OBS overlay</a>.</li>
              <li>You cannot go below the session the break is leading into — that one is already committed. To stop sooner, leave the block or skip the break.</li>
              <li>Focus and break lengths stay fixed for the whole run. Only the session count moves.</li>
            </ul>
            <p class="muted">
              Normal rooms keep their settings locked for the duration of a run: changing a block's
              length under people who are inside one is a different thing from changing how many
              are left.
            </p>
          </section>

          <section v-show="active === 'streaming'" id="streaming" data-docs-section="streaming" class="docs-section">
            <h2>Streaming and overlays</h2>
            <h3>The OBS overlay</h3>
            <p>
              Every temporary room has an overlay twin at <code>/f/{code}/embed</code> —
              <strong>Copy OBS overlay link</strong> on the room screen puts it on your clipboard.
              It is the countdown and nothing else: no chrome, transparent background, and no login,
              so it drops straight into an OBS browser source.
            </p>
            <div class="docs-callout">
              <p>
                Share the <em>plain</em> room link with people and keep the overlay link for your
                own scene. The overlay is read-only and serves temporary rooms only.
              </p>
            </div>
            <h3>The pop-out timer</h3>
            <p>
              The countdown can be popped out into a small always-on-top window, so it stays visible
              while you work in something else. Where the browser supports Document
              Picture-in-Picture it uses that; elsewhere it falls back to a plain popup window.
            </p>
            <h3>Keeping the screen awake</h3>
            <p>
              On supported mobile browsers junkie takes a screen wake lock while a timer is visible
              or a session is running, so the phone is less likely to lock mid-block.
            </p>
          </section>

          <section v-show="active === 'sound'" id="sound" data-docs-section="sound" class="docs-section">
            <h2>Chimes and room sounds</h2>
            <p>
              junkie can play a sound when a block ends. There are two separate pieces, and they
              work together.
            </p>
            <ul>
              <li>
                <strong>The chime toggle is yours, per device.</strong> It decides whether this
                browser makes noise at all, and travels nowhere — turning it on at home does not
                turn it on at work.
              </li>
              <li>
                <strong>The sound is the room's.</strong> A room admin can upload one, and everyone
                with their chime switched on hears it at the end of each block. Without one, junkie
                uses its built-in chime.
              </li>
            </ul>
            <p>
              <strong>Temporary rooms are the exception.</strong> You join one by following a link into
              someone's block, and the chime is part of that block: it plays for everyone there,
              whatever their own toggle says — the room's uploaded sound if it has one, the built-in
              chime otherwise. If you would rather not hear it, mute the tab or leave. One thing no
              website can get around: a browser only plays sound on a page you have touched, so if
              you open the link and never click or tap anything, the first chime may be silent.
              Any tap on the page unlocks it from then on.
            </p>
            <p>
              An uploaded clip can be up to <strong>15 seconds</strong> and <strong>512 KB</strong>,
              as MP3, OGG or WAV. There is no transcoding — what you upload is what plays.
            </p>
            <div class="docs-callout docs-callout-warn">
              <p>
                <strong>WAV is by far the biggest format.</strong> A clip that will not fit as a WAV
                usually fits comfortably as an MP3 or OGG of the same length. Temporary rooms can
                take their sound in the creation popup, before the room exists.
              </p>
            </div>
          </section>

          <section v-show="active === 'connections'" id="connections" data-docs-section="connections" class="docs-section">
            <h2>Connections and profiles</h2>
            <p>
              A connection is a mutual link between two accounts, separate from room membership.
            </p>
            <ul>
              <li>From Profile → Connections, copy a <strong>one-time connect link</strong> and send it. Each link works once; generate a new one for the next person.</li>
              <li>Opening one shows a confirmation screen — nothing is linked until the recipient accepts.</li>
              <li>Once connected you each see the other's <strong>focus heatmap</strong> on <code>/connections</code> and on their profile at <code>/{username}</code>.</li>
              <li>Profiles are invisible to non-connections. There is no public directory.</li>
            </ul>
            <p class="muted">
              There is no in-app way to remove a connection yet, and the feed shows heatmaps only —
              no todos, rooms or timers.
            </p>
          </section>

          <section v-show="active === 'phone'" id="phone" data-docs-section="phone" class="docs-section">
            <h2>On your phone</h2>
            <p>
              junkie is a PWA, so it installs to the home screen and runs fullscreen with no browser
              chrome.
            </p>
            <ul>
              <li><strong>iPhone and iPad</strong> — open it in Safari, then Share → Add to Home Screen.</li>
              <li><strong>Android</strong> — open it in Chrome and tap <strong>Install app</strong>, or download the signed APK from the <a href="https://github.com/samnodier/junkie/releases/tag/android" target="_blank" rel="noopener">android release</a> and open it. You may need to allow installing from unknown sources.</li>
            </ul>
            <p class="muted">
              Both are the same app pointing at the same site; the APK just wraps it so there is
              nothing to install from a browser. Because it is a wrapper, it picks up site updates
              without needing a new APK.
            </p>
          </section>

          <section v-show="active === 'terminal'" id="terminal" data-docs-section="terminal" class="docs-section">
            <h2>Terminal client</h2>
            <p>
              A full-screen terminal desk against the same account and the same rooms: a live
              countdown, your todos, and what your rooms are doing.
            </p>

            <h3>Install</h3>
            <pre class="docs-code"><code>curl -fsSL https://raw.githubusercontent.com/samnodier/junkie/master/install.sh | sh</code></pre>
            <p>
              That puts a <code>junkie</code> binary in <code>~/.local/bin</code>, which needs to be
              on your <code>PATH</code>. It picks the newest release for your platform and checks it
              against the published <code>checksums.txt</code> first. <strong>Re-run the same command
              to upgrade.</strong> <code>JUNKIE_VERSION=vX.Y.Z</code> pins a version;
              <code>JUNKIE_INSTALL_DIR</code> installs somewhere else.
            </p>
            <table class="docs-table">
              <thead><tr><th>Platform</th><th>Supported</th></tr></thead>
              <tbody>
                <tr><td>macOS — Intel and Apple Silicon</td><td>Yes, via the installer</td></tr>
                <tr><td>Linux — x86-64 and ARM64, any distribution</td><td>Yes, via the installer</td></tr>
                <tr><td>WSL</td><td>Yes — it is the Linux build</td></tr>
                <tr><td>Windows, natively</td><td>Builds are published; download the <code>.zip</code> and put <code>junkie.exe</code> on your PATH</td></tr>
              </tbody>
            </table>
            <p class="muted">
              The binaries are statically linked, so there is no glibc or musl requirement — Alpine
              works as well as Fedora. With Go installed you can build from source instead with
              <code>go install github.com/samnodier/junkie/cmd/junkie-cli@latest</code>, which names
              the binary <code>junkie-cli</code> because <code>cmd/junkie</code> is the server.
            </p>

            <h3>Signing in</h3>
            <p>
              You don't have to — <code>junkie</code> on its own opens a guest desk. When you do want
              your account, press <code>L</code> there or run <code>junkie login</code>. It shows a
              short code and waits:
            </p>
            <pre class="docs-code"><code>  your code  2HA5-6AM8

  Approve it at https://junkie-blin.onrender.com/cli
  waiting for approval…</code></pre>
            <p>
              Open that page in a browser where you are already signed in, check the code matches,
              and approve it. The terminal picks the session up a second later.
              <strong>Your password is never typed into the terminal.</strong>
            </p>
            <div class="docs-callout">
              <p>
                <strong>The browser does not have to be on that machine.</strong> On a server you
                reached over SSH, read the code off the screen and approve it on your laptop or your
                phone — the code is what ties the two together. That is also why the page never
                approves anything on its own: the click is the point.
              </p>
            </div>
            <p>
              The session is stored in <code>~/.config/junkie/config.json</code> at mode
              <code>0600</code> and lasts <strong>30 days from signing in</strong> without renewing,
              so roughly once a month you will sign in again. <code>junkie logout</code> ends it
              immediately, on the server as well as on disk, and so does changing your password.
            </p>
            <p class="muted">
              <code>junkie login --password</code> asks for a username and password instead, for a
              self-hosted server too old to offer the browser flow.
            </p>

            <h3>The desk</h3>
            <p>
              Three panes — the block, the todos, the rooms. <code>tab</code> moves between them and
              <code>j</code>/<code>k</code> scroll whichever has the focus, marked with a
              <code>‹</code>.
            </p>
            <table class="docs-table">
              <thead><tr><th>Group</th><th>Key</th><th>Does</th></tr></thead>
              <tbody>
                <tr><td>any</td><td><code>j</code> <code>k</code></td><td>scroll the focused pane</td></tr>
                <tr><td>any</td><td><code>g</code> <code>G</code></td><td>jump to the ends of it</td></tr>
                <tr><td>todos</td><td><code>space</code></td><td>complete or un-complete</td></tr>
                <tr><td>todos</td><td><code>a</code> <code>e</code> <code>d</code> <code>u</code></td><td>add · edit · remove · undo the last remove</td></tr>
                <tr><td>block</td><td><code>f</code> <code>b</code> <code>s</code></td><td>start focus · take the break · skip it</td></tr>
                <tr><td>block</td><td><code>i</code></td><td>I'm in — join a room's block, or check in for the next session</td></tr>
                <tr><td>block</td><td><code>x</code></td><td>end the block on screen — your own, or leave a room's. Asks first either way</td></tr>
                <tr><td>desk</td><td><code>A</code></td><td>join a room by code without leaving the desk</td></tr>
                <tr><td>desk</td><td><code>w</code></td><td>timer pane fills the window (<code>esc</code> or <code>q</code> returns)</td></tr>
                <tr><td>desk</td><td><code>r</code> <code>q</code></td><td>refresh · quit</td></tr>
                <tr><td>desk</td><td><code>L</code></td><td>sign in, from the guest desk</td></tr>
                <tr><td>desk</td><td><code>y</code> <code>n</code></td><td>answer a room's join prompt</td></tr>
              </tbody>
            </table>
            <p>
              Your own block and a room's run independently — you can be in both, and the countdown
              shows one at a time. <code>j</code> and <code>k</code> move between them with the block
              pane focused. Whichever is on screen is named above the digits, so there is never a
              question which one you are looking at, and the todo list follows it.
            </p>
            <p>
              When someone starts a block in one of your rooms, the desk asks whether you want in
              and counts down the 30 seconds you have to answer. Not answering is an answer. This
              only reaches you while the desk is open — when you are away, the Discord bot notifies
              you instead.
            </p>

            <h3>Commands</h3>
            <table class="docs-table">
              <thead><tr><th>Command</th><th>Does</th></tr></thead>
              <tbody>
                <tr><td><code>junkie</code></td><td>open the desk full-screen</td></tr>
                <tr><td><code>junkie CODE</code></td><td>open it on that room's block</td></tr>
                <tr><td><code>w</code> in the desk</td><td>zoom the timer pane to fill the window (<code>esc</code> returns)</td></tr>
                <tr><td><code>junkie status</code></td><td>timer, todo counts and every room's activity in one glance</td></tr>
                <tr><td><code>junkie todos</code></td><td>list your private todos</td></tr>
                <tr><td><code>junkie stats</code></td><td>the work map — a year of focused days</td></tr>
                <tr><td><code>junkie room new NAME</code> · <code>join CODE</code></td><td>create or join a room</td></tr>
                <tr><td><code>junkie room start [CODE] [MINUTES]</code></td><td>start a block, opening the 30-second lobby</td></tr>
                <tr><td><code>junkie room enter</code> · <code>leave</code> · <code>checkin</code> · <code>skip</code></td><td>act on the running block</td></tr>
                <tr><td><code>junkie whoami</code> · <code>logout</code></td><td>who this terminal is · sign out</td></tr>
              </tbody>
            </table>
            <p class="muted">
              Room commands take a code, and you can leave it out when you are only in one room.
              Pasting a room's URL works as well as typing its code.
            </p>

            <h3>Sizing and scripting</h3>
            <p>
              The countdown fits itself to the window, so a terminal parked down the side of a screen
              is a first-class way to run it: it drops to smaller digits, then to a line of text, then
              to the numbers alone, rather than wrapping.
            </p>
            <p>
              Every read command takes <code>--json</code>, piped output is never truncated, and
              <code>junkie</code> with no arguments prints the status instead of opening the desk when
              it is not attached to a terminal. <code>JUNKIE_URL</code> points a command at another
              server, <code>JUNKIE_CONFIG</code> moves the config file, and <code>JUNKIE_DATA</code>
              the guest todos and timer.
            </p>
            <p class="muted">
              Deliberately not in the terminal: avatars, connections, public profiles, the admin
              space, Discord linking, password reset, room settings and temporary rooms. What it does
              is the part a terminal is good at — a block running in a pane while you work.
            </p>
          </section>

          <section v-show="active === 'discord'" id="discord" data-docs-section="discord" class="docs-section">
            <h2>Discord bot</h2>
            <p>
              The bot runs a server's focus room from chat: live countdown messages showing who is
              in, break notifications, Join and break-control buttons, stats and a heatmap picture.
            </p>
            <p>
              <strong>Just want to link your account or reset a password?</strong>
              <a href="https://discord.gg/qEEzdXQHtK" target="_blank" rel="noopener">Join the junkie Discord server</a>
              and use <code>/junkie link</code> or <code>/junkie reset-password</code> in
              <code>#junkie-bot</code>. Your commands and the bot's replies there are private.
            </p>
            <h3>Adding it to your own server</h3>
            <p>
              Needs Manage Server permission. The bot only asks for Send Messages. Once it is in:
              each participant runs <code>/junkie link</code> once, then an admin runs
              <code>/junkie register</code> in the channel the timer should post to — pass a room
              code to connect an existing room, or omit it to create a fresh one. Move notifications
              later with <code>/junkie channel [#channel]</code>, and <code>/junkie help</code> lists
              everything.
            </p>
            <h3>Settings and controls</h3>
            <p>
              <code>/junkie config</code> manages room settings: the timer as
              <code>timer:30/5/3</code>, plus <code>checkin:On/Off</code> and
              <code>auto-breaks:On/Off</code>. Supply only what you want to change; run it bare to
              see the current settings. As on the web, settings cannot change mid-run.
            </p>
            <p>
              During a break the live message carries the same controls the web room has:
              <strong>Start break</strong> when one is waiting, <strong>Pause</strong>,
              <strong>Resume</strong>, and <strong>Skip break</strong> (hidden in check-in rooms,
              same as the web). Using any of them requires a linked account that is a room member,
              and counts as your check-in.
            </p>
            <p>
              The live message states each phase's length and its deadline both as a wall-clock time
              and as a countdown — “session 1 of 2 · 80 min. Break at 21:19 · in an hour” — each in
              your own timezone. Both are needed: the countdown alone rounds to one coarse unit, so a
              long block reads as “in an hour” for most of its length.
            </p>
            <div class="docs-callout">
              <p>
                Self-hosting? The bot is optional. It starts only when <code>DISCORD_BOT_TOKEN</code>,
                <code>DISCORD_APPLICATION_ID</code> and <code>PUBLIC_BASE_URL</code> are set, and you
                would mint your own invite link with your application's client id.
              </p>
            </div>
          </section>

          <section v-show="active === 'api'" id="api" data-docs-section="api" class="docs-section">
            <h2>Adding todos by API</h2>
            <p>
              An API key lets something other than you — a meeting-notes automation, a script, a
              shortcut on your phone — add todos to one of your lists. Make one under
              <a href="/profile">Profile → API keys</a>, and choose whether it adds to your private
              todos or to your todos in one room. The key is shown once; copy it then.
            </p>
            <p>
              <strong>A key can only add.</strong> It cannot read your todos, complete them, or delete
              them, so a key that leaks costs you some junk todos and nothing else. Revoke it from
              the same place and it stops working at once.
            </p>

            <h3>The request</h3>
            <pre class="docs-code"><code>POST /api/todos
Authorization: Bearer jk_…
Content-Type: application/json
Idempotency-Key: zoom-meeting-987654321   (optional)

{"todos": [
  {"text": "Acme: Send revised October email campaign - Sep 26"},
  {"text": "Acme: Check GHL campaign timezone - Sep 26"}
]}</code></pre>
            <p>
              A successful call answers <code>201</code> with the todos it added. Either every todo
              in the request is added or none is.
            </p>
            <table class="docs-table">
              <thead><tr><th>Rule</th><th>Limit</th></tr></thead>
              <tbody>
                <tr><td>Todos per request</td><td>1 to 20</td></tr>
                <tr><td>Length of one todo</td><td>500 characters — longer is refused, not cut</td></tr>
                <tr><td>Requests per key</td><td>10 a minute, 100 a day — over that is <code>429</code> with <code>Retry-After</code></td></tr>
                <tr><td>Keys per account</td><td>10</td></tr>
              </tbody>
            </table>
            <p>
              Line breaks and control characters in a todo become spaces: todos are one line, as they
              are when typed.
            </p>

            <h3>Retrying safely</h3>
            <p>
              If a call times out you can't tell whether it landed. Send the same
              <code>Idempotency-Key</code> header on the retry — anything unique to the batch, like
              the meeting's id — and junkie answers with the original response instead of adding the
              todos twice (the reply carries <code>Idempotent-Replayed: true</code>). A key is
              remembered for 24 hours. Reusing one with different todos is refused with
              <code>422</code>.
            </p>

            <h3>Errors</h3>
            <table class="docs-table">
              <thead><tr><th>Status</th><th>Meaning</th></tr></thead>
              <tbody>
                <tr><td><code>400</code></td><td>The body is wrong; <code>error</code> says how</td></tr>
                <tr><td><code>401</code></td><td>Missing, mistyped or revoked key</td></tr>
                <tr><td><code>403</code></td><td>The key is for a room you've since left</td></tr>
                <tr><td><code>422</code></td><td>Idempotency key reused with different todos</td></tr>
                <tr><td><code>429</code></td><td>Too many requests; wait for <code>Retry-After</code> seconds</td></tr>
              </tbody>
            </table>
          </section>

          <section v-show="active === 'data'" id="data" data-docs-section="data" class="docs-section">
            <h2>Where your data lives</h2>
            <p>
              <strong>As a guest</strong> — in the browser, todos, solo timer state and your work map
              stay in <code>localStorage</code> on that device. In the terminal they live in
              <code>~/.local/share/junkie/</code>. Nothing reaches the server, and the two guest
              stores do not share with each other.
            </p>
            <p>
              <strong>Signed in</strong> — everything lives in PostgreSQL and follows you between
              devices: account and session, todos, timer runs and focus minutes, rooms and their
              settings, profile pictures, connections.
            </p>
            <p>
              <strong>Finished todos do not stick around.</strong> Once a todo has been completed or
              removed for 24 hours it is deleted permanently, for everyone, in private lists and
              rooms alike. Un-completing or restoring it within that day resets the clock. The work
              map keeps the focus history; the list stays about what is next.
            </p>
            <p class="muted">
              junkie stores no email address, no real name, no analytics, no tracking and no ads, and
              nothing is sold or shared. One cookie keeps you signed in — that is the only cookie
              there is. See <a href="/privacy">Privacy</a> and <a href="/terms">Terms</a>.
            </p>
          </section>

          <section v-show="active === 'admin'" id="admin" data-docs-section="admin" class="docs-section">
            <h2>Admin and owner access</h2>
            <p>
              Site-wide account roles, separate from the per-room roles under
              <a href="#room-people" @click.prevent="go('room-people')">Members, admins, ownership</a>.
            </p>
            <table class="docs-table">
              <thead><tr><th>Role</th><th>Sees</th></tr></thead>
              <tbody>
                <tr><td><code>user</code></td><td>The default. Cannot open <code>/admin</code>.</td></tr>
                <tr><td><code>admin</code></td><td>Operational aggregates, account and room metadata, room deletion.</td></tr>
                <tr><td><code>owner</code></td><td>The above, plus per-user focus summaries and promoting or demoting admins.</td></tr>
              </tbody>
            </table>
            <p>Admin actions are audited.</p>
          </section>

          <section v-show="active === 'selfhost'" id="selfhost" data-docs-section="selfhost" class="docs-section">
            <h2>Self-hosting</h2>
            <p>
              junkie is one Go binary with the frontend embedded in it, plus PostgreSQL. Running it
              yourself, developing against it and deploying it are covered in the
              <a href="https://github.com/samnodier/junkie#local-development" target="_blank" rel="noopener">README</a>,
              which is the right document for anyone touching the code.
            </p>
            <p class="muted">
              Found something wrong on this page, or missing from it?
              <a href="https://github.com/samnodier/junkie/issues" target="_blank" rel="noopener">Open an issue</a>.
            </p>
          </section>
        </div>
      </div>
    </div>
  </AppShell>
</template>
