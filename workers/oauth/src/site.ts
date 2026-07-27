const googleSiteVerification = "fyB0-L8lIT4f1VbiEdZKmxgvBNOTK1hcKM-CZATx4y0"
const supportEmail = "yangglive@gmail.com"
const downloadURL = "https://github.com/teatak/pudding/releases/latest"
const repositoryURL = "https://github.com/teatak/pudding"

type PageOptions = {
  title: string
  description: string
  pathname: string
  content: string
  bodyClass?: string
}

export function publicPage(url: URL): Response | null {
  switch (normalizePath(url.pathname)) {
    case "/":
      return html(homePage(url.origin))
    case "/privacy":
      return html(privacyPage(url.origin))
    case "/terms":
      return html(termsPage(url.origin))
    case "/support":
      return html(supportPage(url.origin))
    case "/data-deletion":
      return html(dataDeletionPage(url.origin))
    case "/robots.txt":
      return text(`User-agent: *\nAllow: /\nSitemap: ${url.origin}/sitemap.xml\n`)
    case "/sitemap.xml":
      return xml(sitemap(url.origin))
    default:
      return null
  }
}

function homePage(origin: string): PageOptions {
  return {
    title: "Pudding",
    description:
      "Pudding is a local-first desktop AI workspace for research, writing, coding, and connected work.",
    pathname: "/",
    bodyClass: "home",
    content: `
      <main>
        <section class="hero shell" aria-labelledby="hero-title" aria-describedby="hero-purpose">
          <div class="hero-copy reveal">
            <div class="eyebrow"><span class="status-dot"></span> Local-first desktop AI</div>
            <h1 id="hero-title">Pudding</h1>
            <p class="hero-tagline">Your AI workspace, close to home.</p>
            <p class="hero-lede" id="hero-purpose"><strong>Pudding is a local-first desktop AI application for macOS.</strong> It helps users research information, write documents, work with code, organize projects, and connect optional services such as Gmail and GitHub from one desktop workspace.</p>
            <div class="hero-actions">
              <a class="button primary" href="${downloadURL}">Download for macOS <span aria-hidden="true">↗</span></a>
              <a class="button secondary" href="${repositoryURL}">View on GitHub</a>
            </div>
            <p class="hero-note"><span aria-hidden="true">●</span> Public homepage · No sign-in required</p>
          </div>

          <div class="product-stage reveal reveal-delay" aria-label="A preview of the Pudding desktop workspace">
            <div class="glow glow-one"></div>
            <div class="glow glow-two"></div>
            <div class="app-window">
              <div class="window-bar">
                <div class="traffic" aria-hidden="true"><i></i><i></i><i></i></div>
                <div class="window-title"><img src="/logo.png" alt=""> Pudding</div>
                <div class="window-chip">Local</div>
              </div>
              <div class="app-body">
                <aside class="session-rail" aria-hidden="true">
                  <div class="new-session">＋&nbsp; New task</div>
                  <div class="rail-label">TODAY</div>
                  <div class="session active"><b>Launch brief</b><small><i></i> Working · now</small></div>
                  <div class="session"><b>Release notes</b><small>Completed · 12m</small></div>
                  <div class="session"><b>Inbox review</b><small>Idle · 1h</small></div>
                </aside>
                <div class="task-pane">
                  <div class="pane-heading"><span>Launch brief</span><span class="pane-tools">•••</span></div>
                  <div class="timeline">
                    <div class="user-task">Turn these research notes into a concise launch brief.</div>
                    <div class="work-step"><span class="step-icon">✓</span><span><b>Read project notes</b><small>8 files reviewed locally</small></span></div>
                    <div class="work-step"><span class="step-icon indigo">↗</span><span><b>Compare key themes</b><small>Audience, value, proof points</small></span></div>
                    <div class="answer-block">
                      <div class="answer-meta"><span></span> Pudding · Draft ready</div>
                      <strong>A calmer way to move complex work forward.</strong>
                      <p>Pudding keeps conversations, tools, and project context together—without turning your desktop into another cloud dashboard.</p>
                    </div>
                  </div>
                  <div class="composer"><span>Ask a follow-up…</span><b>↑</b></div>
                </div>
              </div>
            </div>
          </div>
        </section>

        <section class="proof-strip" aria-label="Pudding principles">
          <div class="shell proof-grid">
            <div><strong>Local-first</strong><span>Your app data lives on your computer.</span></div>
            <div><strong>Multi-session</strong><span>Keep independent tasks moving in parallel.</span></div>
            <div><strong>Your providers</strong><span>Choose the AI and services you connect.</span></div>
          </div>
        </section>

        <section class="section shell" id="product">
          <div class="section-heading">
            <p class="overline">Built for real work</p>
            <h2>One place for the whole working process.</h2>
            <p>Pudding is a desktop agent workspace, not just a chat box. It keeps research, tool activity, drafts, and long-running tasks understandable from start to finish.</p>
          </div>
          <div class="feature-grid">
            <article class="feature-card large">
              <div class="feature-visual sessions-visual" aria-hidden="true">
                <div class="mini-rail"><i></i><i></i><i></i><i></i></div>
                <div class="mini-pane"><span></span><span></span><span></span></div>
                <div class="mini-pane second"><span></span><span></span><span></span></div>
              </div>
              <p class="card-number">01</p>
              <h3>Parallel by design</h3>
              <p>Each conversation has its own context and tools, so one task can keep running while you focus on another.</p>
            </article>
            <article class="feature-card">
              <div class="feature-visual local-visual" aria-hidden="true">
                <div class="device"><span></span><i></i></div>
                <div class="orbit orbit-a"></div><div class="orbit orbit-b"></div>
              </div>
              <p class="card-number">02</p>
              <h3>Local is the default</h3>
              <p>Sessions, settings, and app connections are managed by the desktop app on your device.</p>
            </article>
            <article class="feature-card">
              <div class="feature-visual connect-visual" aria-hidden="true">
                <span class="connect-node center"><img src="/logo.png" alt=""></span>
                <span class="connect-node node-a">G</span><span class="connect-node node-b">⌘</span><span class="connect-node node-c">AI</span>
              </div>
              <p class="card-number">03</p>
              <h3>Connect deliberately</h3>
              <p>Add Gmail, GitHub, model providers, and other tools only when they are useful to you.</p>
            </article>
          </div>
        </section>

        <section class="section shell connect-section" id="connections">
          <div class="connection-copy">
            <p class="overline">Connected, with boundaries</p>
            <h2>Your inbox stays read-only.</h2>
            <p>When you connect Gmail, Pudding requests read-only access so it can search and read messages when you ask. It cannot send, edit, or delete your email.</p>
            <a class="text-link" href="/privacy">See exactly how Google data is handled <span aria-hidden="true">→</span></a>
          </div>
          <div class="permission-card">
            <div class="permission-top"><span class="google-mark">G</span><div><b>Gmail connection</b><small>Optional · user initiated</small></div><span class="readonly">READ-ONLY</span></div>
            <div class="permission-list">
              <div><span>✓</span><p><b>View your email messages</b><small>Only when you use a Gmail feature</small></p></div>
              <div><span>✓</span><p><b>See basic account information</b><small>Name, email address, and profile</small></p></div>
              <div class="blocked"><span>—</span><p><b>Send, edit, or delete mail</b><small>Never requested</small></p></div>
            </div>
            <p class="permission-foot">OAuth credentials are returned to Pudding on your device. Cloud handoff records are short-lived and are deleted after redemption or expiry.</p>
          </div>
        </section>

        <section class="cta-section shell">
          <div>
            <p class="overline light">Desktop AI, on your terms</p>
            <h2>Make room for deeper work.</h2>
            <p>Bring conversations, projects, and tools into one calm workspace.</p>
          </div>
          <a class="button light-button" href="${downloadURL}">Get Pudding <span aria-hidden="true">↗</span></a>
        </section>
      </main>`,
  }
}

function privacyPage(origin: string): PageOptions {
  return legalPage(origin, {
    pathname: "/privacy",
    title: "Privacy Policy — Pudding",
    description: "How Pudding collects, uses, stores, and shares data, including Google user data.",
    eyebrow: "Your data, explained",
    heading: "Privacy Policy",
    intro:
      "Pudding is a local-first AI workspace. This policy explains how the Pudding apps and the OAuth services at x-t.top and oauth.x-t.top handle information.",
    content: `
      <section id="scope"><h2>1. Scope</h2><p>This policy applies to Pudding for desktop and the public website and OAuth exchange service hosted at <a href="${origin}">${escapeHTML(origin)}</a>. Pudding does not require a website account.</p></section>
      <section id="information"><h2>2. Information Pudding handles</h2>
        <h3>Information on your device</h3><p>Pudding stores app settings, conversations, project references, and connection credentials in its local application data on your computer. You decide which projects and services to connect.</p>
        <h3>Google account information</h3><p>If you choose to connect Gmail, Pudding requests your basic Google profile (name, email address, and profile information) and the <code>gmail.readonly</code> permission. This permits Pudding to search and read Gmail messages. It does not permit Pudding to send, modify, or delete email.</p>
        <h3>Service metadata</h3><p>Our hosting and security providers may process standard request metadata such as IP address, timestamp, user agent, and requested URL to deliver and protect the service. We do not use advertising cookies or third-party analytics on this website.</p>
      </section>
      <section id="use"><h2>3. How information is used</h2><p>Google user data is used only to provide user-requested Gmail features, such as finding, reading, organizing, or summarizing messages inside Pudding. Basic profile information identifies the account you connected.</p><p>Pudding does not sell Google user data, use it for advertising, or use Gmail content to train a general-purpose AI model. Pudding's use and transfer of information received from Google APIs complies with the <a href="https://developers.google.com/terms/api-services-user-data-policy">Google API Services User Data Policy</a>, including the Limited Use requirements.</p></section>
      <section id="oauth"><h2>4. OAuth and credential handling</h2><p>During a GitHub connection, GitHub returns an authorization code to <code>x-t.top</code>. The service exchanges that code for tokens and keeps the result only during a short device-handoff window. Pudding receives a single-use ticket, redeems it over HTTPS, and the handoff record is then deleted. The legacy Gmail flow sends an authorization code to <code>oauth.x-t.top</code> solely to exchange it for tokens.</p><p>Connection credentials are stored locally by Pudding on your device. Treat access to your operating-system account as sensitive and keep your device secured.</p></section>
      <section id="sharing"><h2>5. Data sharing</h2><p>Pudding does not sell personal information. Information is disclosed only as needed to provide a feature you request:</p><ul><li><strong>Google</strong> processes authorization and Gmail API requests.</li><li><strong>Cloudflare</strong> hosts this website and the short-lived OAuth token exchange.</li><li><strong>Your chosen AI provider</strong> may receive message excerpts or derived context when you explicitly ask Pudding to use Gmail content in an AI task. That provider's terms and privacy policy apply.</li><li><strong>Legal and safety requests</strong> may require disclosure where applicable law demands it.</li></ul><p>We do not allow humans to read Google user data except with your affirmative agreement for support or security, when necessary to investigate abuse, or when required by law.</p></section>
      <section id="retention"><h2>6. Retention and deletion</h2><p>GitHub OAuth handoff records expire within minutes and are deleted immediately after successful redemption. The legacy exchange service does not retain Google tokens or Gmail content. Locally stored credentials remain until you remove the connection, delete Pudding's local data, or revoke access from the connected service.</p><p>See the <a href="/data-deletion">data deletion guide</a> for step-by-step options.</p></section>
      <section id="security"><h2>7. Security</h2><p>We use HTTPS for network requests, fixed allowlisted application return schemes, random state values, client-bound challenges, short-lived single-use tickets, and no persistent server-side token database. No security measure is perfect; please report suspected issues to <a href="mailto:${supportEmail}">${supportEmail}</a>.</p></section>
      <section id="choices"><h2>8. Your choices</h2><p>Connecting Gmail is optional. You can decline a requested permission, remove the Gmail connection in Pudding, revoke access in Google, or stop using the feature at any time.</p></section>
      <section id="children"><h2>9. Children</h2><p>Pudding is not directed to children under 13, and we do not knowingly collect personal information from children through this website.</p></section>
      <section id="changes"><h2>10. Changes and contact</h2><p>We may update this policy as the product changes. The date at the top identifies the latest revision. Questions or privacy requests can be sent to <a href="mailto:${supportEmail}">${supportEmail}</a>.</p></section>`,
  })
}

function termsPage(origin: string): PageOptions {
  return legalPage(origin, {
    pathname: "/terms",
    title: "Terms of Service — Pudding",
    description: "Terms governing use of Pudding and its OAuth exchange service.",
    eyebrow: "Clear expectations",
    heading: "Terms of Service",
    intro:
      "These terms govern your use of Pudding and the supporting public services at x-t.top and oauth.x-t.top. By using them, you agree to these terms.",
    content: `
      <section><h2>1. The service</h2><p>Pudding is a local-first AI workspace. Its public OAuth service helps Pudding apps connect user-authorized services such as Gmail and GitHub. Features may change as Pudding evolves.</p></section>
      <section><h2>2. Your accounts and permissions</h2><p>You may connect only accounts you own or are authorized to use. You are responsible for your device, connected accounts, chosen AI providers, and actions you approve through Pudding. You may disconnect a service at any time.</p></section>
      <section><h2>3. Acceptable use</h2><p>Do not use Pudding or its OAuth service to violate law, infringe rights, access accounts without authorization, distribute malware, interfere with the service, or bypass security and usage controls.</p></section>
      <section><h2>4. Third-party services</h2><p>Pudding can interact with services operated by others, including Google, GitHub, Cloudflare, and AI model providers you configure. Their own terms and privacy policies govern their services. We are not responsible for third-party availability or changes.</p></section>
      <section><h2>5. Software and content</h2><p>You keep ownership of your content. You are responsible for reviewing AI-generated output before relying on or publishing it. Do not assume generated output is complete, accurate, or suitable for high-stakes decisions.</p></section>
      <section><h2>6. Availability and changes</h2><p>The service is provided on an “as available” basis. We may modify, suspend, or discontinue features to maintain security, comply with law, or improve the product.</p></section>
      <section><h2>7. Disclaimer</h2><p>To the maximum extent permitted by law, Pudding is provided without warranties of merchantability, fitness for a particular purpose, or non-infringement. We do not guarantee uninterrupted operation or error-free output.</p></section>
      <section><h2>8. Limitation of liability</h2><p>To the maximum extent permitted by law, we will not be liable for indirect, incidental, special, consequential, or exemplary damages, or for loss of data, profits, or business arising from use of the service.</p></section>
      <section><h2>9. Termination</h2><p>You may stop using Pudding at any time. We may restrict access to public services when necessary to prevent abuse, protect users, or comply with law.</p></section>
      <section><h2>10. Contact</h2><p>Questions about these terms can be sent to <a href="mailto:${supportEmail}">${supportEmail}</a>. See the <a href="${origin}/privacy">Privacy Policy</a> for data-handling details.</p></section>`,
  })
}

function supportPage(origin: string): PageOptions {
  return legalPage(origin, {
    pathname: "/support",
    title: "Support — Pudding",
    description: "Help with Pudding, connected accounts, Gmail permissions, and privacy requests.",
    eyebrow: "Help when you need it",
    heading: "Pudding Support",
    intro:
      "Find quick answers about installation, Gmail access, account connections, and data controls.",
    content: `
      <section><h2>Download and updates</h2><p>Get the latest macOS build from the <a href="${downloadURL}">Pudding releases page</a>. Packaged builds check for updates automatically and wait for you to restart before installing them.</p></section>
      <section><h2>Why does Pudding ask for Gmail access?</h2><p>Gmail is an optional Pudding connection. If you enable it, Pudding requests <code>gmail.readonly</code> so it can find and read messages when you ask. Pudding cannot send, edit, or delete email with this permission.</p></section>
      <section><h2>How do I disconnect Gmail?</h2><ol><li>Remove the Gmail connection inside Pudding.</li><li>Open <a href="https://myaccount.google.com/permissions">Google Account permissions</a>.</li><li>Select Pudding and revoke access.</li></ol><p>For local data removal, follow the <a href="/data-deletion">data deletion guide</a>.</p></section>
      <section><h2>OAuth connection problems</h2><p>Confirm that you selected the intended account and repositories, accepted only the permissions you want, and returned to the Pudding app after authorization. If the callback does not complete, close the authorization tab and start the connection again from Pudding.</p></section>
      <section><h2>Contact</h2><p>Email <a href="mailto:${supportEmail}">${supportEmail}</a> with a concise description, your Pudding version, and your macOS version. Do not send passwords, access tokens, full email content, or other secrets.</p><p>You can also review the public project at <a href="${repositoryURL}">GitHub</a>.</p></section>`,
  })
}

function dataDeletionPage(origin: string): PageOptions {
  return legalPage(origin, {
    pathname: "/data-deletion",
    title: "Data Deletion — Pudding",
    description: "How to remove connected-account access and locally stored Pudding data.",
    eyebrow: "You stay in control",
    heading: "Data Deletion",
    intro:
      "Pudding keeps its application data on your device, while OAuth handoff records are short-lived and deleted after redemption or expiry.",
    content: `
      <section><h2>Remove a Gmail connection</h2><ol><li>Open Pudding and remove the Gmail connection you no longer want to use.</li><li>Visit <a href="https://myaccount.google.com/permissions">Google Account permissions</a>.</li><li>Select Pudding and choose the option to remove access.</li></ol><p>Revoking access invalidates Pudding's authorization to call Gmail APIs for that Google account.</p></section>
      <section><h2>Delete local conversations and settings</h2><p>Delete individual conversations in Pudding when you no longer need them. To remove all Pudding application data, quit the app, remove its local Pudding data directory, and uninstall the application. This action is destructive and cannot be undone.</p></section>
      <section><h2>Server-side data</h2><p>The GitHub OAuth handoff at <code>x-t.top</code> expires within minutes and is deleted immediately after successful redemption. The legacy exchange at <code>oauth.x-t.top</code> does not persist authorization codes, Google tokens, or Gmail content. There is no Pudding account database to delete from this website.</p></section>
      <section><h2>Need help?</h2><p>For a privacy or deletion question, email <a href="mailto:${supportEmail}">${supportEmail}</a>. Do not include passwords, OAuth tokens, or email contents in your message. See the <a href="${origin}/privacy">Privacy Policy</a> for more detail.</p></section>`,
  })
}

type LegalOptions = {
  pathname: string
  title: string
  description: string
  eyebrow: string
  heading: string
  intro: string
  content: string
}

function legalPage(_origin: string, options: LegalOptions): PageOptions {
  return {
    title: options.title,
    description: options.description,
    pathname: options.pathname,
    bodyClass: "legal",
    content: `
      <main class="legal-main shell">
        <header class="legal-hero">
          <p class="overline">${escapeHTML(options.eyebrow)}</p>
          <h1>${escapeHTML(options.heading)}</h1>
          <p>${escapeHTML(options.intro)}</p>
          <div class="updated">Last updated: July 27, 2026</div>
        </header>
        <article class="legal-content">${options.content}</article>
      </main>`,
  }
}

function html(options: PageOptions): Response {
  const canonical = `https://x-t.top${options.pathname}`
  const document = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="application-name" content="Pudding">
  <meta name="apple-mobile-web-app-title" content="Pudding">
  <meta name="theme-color" content="#f7f7f5">
  <meta name="description" content="${escapeHTML(options.description)}">
  <meta name="google-site-verification" content="${escapeHTML(googleSiteVerification)}">
  <link rel="canonical" href="${canonical}">
  <link rel="icon" href="/logo.png" type="image/png">
  <meta property="og:type" content="website">
  <meta property="og:site_name" content="Pudding">
  <meta property="og:title" content="${escapeHTML(options.title)}">
  <meta property="og:description" content="${escapeHTML(options.description)}">
  <meta property="og:url" content="${canonical}">
  <meta property="og:image" content="https://x-t.top/og.png">
  <meta property="og:image:alt" content="Pudding — Local-first AI workspace">
  <meta name="twitter:card" content="summary_large_image">
  <meta name="twitter:title" content="${escapeHTML(options.title)}">
  <meta name="twitter:description" content="${escapeHTML(options.description)}">
  <meta name="twitter:image" content="https://x-t.top/og.png">
  <title>${escapeHTML(options.title)}</title>
  <style>${styles}</style>
</head>
<body class="${options.bodyClass ?? ""}">
  ${siteHeader(options.pathname)}
  ${options.content}
  ${siteFooter()}
</body>
</html>`

  return new Response(document, {
    status: 200,
    headers: pageHeaders("text/html; charset=utf-8"),
  })
}

function siteHeader(pathname: string): string {
  return `<header class="site-header">
    <nav class="shell nav" aria-label="Main navigation">
      <a class="brand" href="/" aria-label="Pudding home"><img src="/logo.png" alt=""><span>Pudding</span></a>
      <div class="nav-links">
        <a href="/#product">Product</a>
        ${navLink("/privacy", "Privacy", pathname)}
        ${navLink("/support", "Support", pathname)}
      </div>
      <a class="nav-download" href="${downloadURL}">Download <span aria-hidden="true">↗</span></a>
    </nav>
  </header>`
}

function siteFooter(): string {
  return `<footer class="site-footer">
    <div class="shell footer-grid">
      <div class="footer-brand"><a class="brand" href="/"><img src="/logo.png" alt=""><span>Pudding</span></a><p>A local-first desktop AI workspace.</p></div>
      <div class="footer-links"><div><b>Product</b><a href="/#product">What it does</a><a href="${downloadURL}">Download</a><a href="${repositoryURL}">GitHub</a></div><div><b>Trust</b><a href="/privacy">Privacy</a><a href="/terms">Terms</a><a href="/data-deletion">Data deletion</a></div><div><b>Help</b><a href="/support">Support</a><a href="mailto:${supportEmail}">Contact</a></div></div>
    </div>
    <div class="shell footer-bottom"><span>© 2026 Pudding</span><span>Designed for work that stays yours.</span></div>
  </footer>`
}

function navLink(path: string, label: string, pathname: string): string {
  const current = pathname === path ? ' aria-current="page"' : ""
  return `<a href="${path}"${current}>${label}</a>`
}

function sitemap(origin: string): string {
  const pages = ["/", "/privacy", "/terms", "/support", "/data-deletion"]
  return `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${pages.map((path) => `\n  <url><loc>${escapeXML(`${origin}${path}`)}</loc></url>`).join("")}\n</urlset>\n`
}

function text(body: string): Response {
  return new Response(body, { status: 200, headers: pageHeaders("text/plain; charset=utf-8") })
}

function xml(body: string): Response {
  return new Response(body, {
    status: 200,
    headers: pageHeaders("application/xml; charset=utf-8"),
  })
}

function pageHeaders(contentType: string): HeadersInit {
  return {
    "Content-Type": contentType,
    "Cache-Control": "public, max-age=300",
    "Content-Security-Policy": "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
    "Permissions-Policy": "camera=(), microphone=(), geolocation=(), payment=()",
    "Referrer-Policy": "strict-origin-when-cross-origin",
    "X-Content-Type-Options": "nosniff",
    "X-Frame-Options": "DENY",
  }
}

function normalizePath(pathname: string): string {
  if (!pathname || pathname === "/") return "/"
  return pathname.endsWith("/") ? pathname.slice(0, -1) : pathname
}

function escapeHTML(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;")
}

function escapeXML(value: string): string {
  return escapeHTML(value)
}

const styles = String.raw`
  :root {
    color-scheme: light;
    --ink: #1d1d20;
    --muted: #66666f;
    --soft: #f1f1ef;
    --paper: #f7f7f5;
    --white: #fff;
    --line: rgba(29, 29, 32, .11);
    --indigo: #4d3ce0;
    --indigo-deep: #3526bd;
    --indigo-soft: #eeecff;
    --green: #14825f;
    --radius: 22px;
  }
  * { box-sizing: border-box; }
  html { scroll-behavior: smooth; }
  body { margin: 0; background: var(--paper); color: var(--ink); font-family: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; font-size: 16px; line-height: 1.65; -webkit-font-smoothing: antialiased; }
  a { color: inherit; text-underline-offset: 3px; }
  img { display: block; max-width: 100%; }
  .shell { width: min(1160px, calc(100% - 48px)); margin-inline: auto; }
  .site-header { position: relative; z-index: 10; border-bottom: 1px solid var(--line); background: color-mix(in srgb, var(--paper) 88%, transparent); backdrop-filter: blur(16px); }
  .nav { min-height: 76px; display: grid; grid-template-columns: 1fr auto 1fr; align-items: center; gap: 32px; }
  .brand { width: max-content; display: inline-flex; align-items: center; gap: 10px; color: var(--ink); text-decoration: none; font-weight: 760; letter-spacing: -.02em; }
  .brand img { width: 34px; height: 34px; border-radius: 9px; }
  .nav-links { display: flex; align-items: center; gap: 30px; }
  .nav-links a { color: var(--muted); font-size: 14px; font-weight: 590; text-decoration: none; }
  .nav-links a:hover, .nav-links a[aria-current="page"] { color: var(--ink); }
  .nav-download { justify-self: end; border: 1px solid var(--line); border-radius: 999px; background: var(--white); padding: 9px 16px; text-decoration: none; font-size: 14px; font-weight: 650; box-shadow: 0 3px 10px rgba(20,20,24,.04); transition: transform .2s ease, border-color .2s ease; }
  .nav-download:hover { transform: translateY(-1px); border-color: rgba(77,60,224,.35); }
  .hero { min-height: 720px; display: grid; grid-template-columns: .9fr 1.25fr; gap: 60px; align-items: center; padding-block: 82px 92px; }
  .eyebrow { width: max-content; display: flex; align-items: center; gap: 9px; margin-bottom: 26px; border: 1px solid rgba(77,60,224,.16); border-radius: 999px; background: rgba(255,255,255,.68); padding: 7px 12px; color: #514e63; font-size: 12px; font-weight: 700; letter-spacing: .07em; text-transform: uppercase; }
  .status-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--indigo); box-shadow: 0 0 0 4px rgba(77,60,224,.1); }
  h1, h2, h3, p { margin-top: 0; }
  h1, h2, h3 { text-wrap: balance; }
  .hero h1 { max-width: 580px; margin-bottom: 12px; color: var(--indigo); font-size: clamp(48px, 5vw, 76px); line-height: .99; letter-spacing: -.065em; font-weight: 650; }
  .hero-tagline { max-width: 620px; margin-bottom: 24px; font-size: clamp(32px, 3.6vw, 54px); line-height: 1.04; letter-spacing: -.052em; font-weight: 630; text-wrap: balance; }
  .hero-lede { max-width: 600px; margin-bottom: 30px; color: var(--muted); font-size: 17px; line-height: 1.7; }
  .hero-lede strong { color: #45454d; font-weight: 680; }
  .hero-actions { display: flex; flex-wrap: wrap; gap: 12px; }
  .button { min-height: 48px; display: inline-flex; align-items: center; justify-content: center; gap: 10px; border-radius: 999px; padding: 0 20px; text-decoration: none; font-size: 14px; font-weight: 700; transition: transform .2s ease, box-shadow .2s ease, background .2s ease; }
  .button:hover { transform: translateY(-2px); }
  .primary { background: var(--ink); color: #fff; box-shadow: 0 10px 26px rgba(26,25,35,.16); }
  .primary:hover { box-shadow: 0 13px 30px rgba(26,25,35,.22); }
  .secondary { border: 1px solid var(--line); background: rgba(255,255,255,.72); }
  .secondary:hover { background: var(--white); }
  .hero-note { display: flex; align-items: center; gap: 8px; margin: 22px 0 0; color: #83838a; font-size: 12px; }
  .hero-note span { color: var(--green); font-size: 9px; }
  .product-stage { position: relative; min-width: 0; }
  .glow { position: absolute; border-radius: 50%; filter: blur(28px); opacity: .72; }
  .glow-one { width: 280px; height: 280px; right: -50px; top: -80px; background: #d7d2ff; }
  .glow-two { width: 220px; height: 220px; left: -40px; bottom: -80px; background: #eceafa; }
  .app-window { position: relative; overflow: hidden; aspect-ratio: 1.26; border: 1px solid rgba(32,31,39,.18); border-radius: 20px; background: #fff; box-shadow: 0 34px 90px rgba(36,33,57,.19), 0 2px 8px rgba(36,33,57,.06); transform: perspective(1200px) rotateY(-2deg) rotateX(1deg); }
  .window-bar { height: 50px; display: grid; grid-template-columns: 1fr auto 1fr; align-items: center; padding: 0 16px; border-bottom: 1px solid #e9e9e7; background: #fafafa; }
  .traffic { display: flex; gap: 7px; }
  .traffic i { width: 9px; height: 9px; border-radius: 50%; background: #f17168; }
  .traffic i:nth-child(2) { background: #eebb49; }.traffic i:nth-child(3) { background: #61b969; }
  .window-title { display: flex; align-items: center; gap: 7px; color: #55555d; font-size: 11px; font-weight: 650; }
  .window-title img { width: 18px; height: 18px; border-radius: 5px; }
  .window-chip { justify-self: end; border: 1px solid #dfdfdc; border-radius: 999px; padding: 3px 8px; color: #77777f; font-size: 9px; font-weight: 700; }
  .app-body { height: calc(100% - 50px); display: grid; grid-template-columns: 31% 1fr; }
  .session-rail { padding: 14px; border-right: 1px solid #ececea; background: #f4f4f2; }
  .new-session { margin-bottom: 20px; border: 1px solid #dededb; border-radius: 9px; background: #fff; padding: 8px 9px; font-size: 9px; font-weight: 700; }
  .rail-label { margin: 0 5px 7px; color: #9b9b9e; font-size: 7px; font-weight: 800; letter-spacing: .11em; }
  .session { display: flex; flex-direction: column; gap: 2px; margin-bottom: 4px; border-radius: 9px; padding: 8px 9px; color: #55555d; }
  .session.active { background: #e7e5fb; color: #252239; }
  .session b { font-size: 9px; line-height: 1.2; }.session small { display: flex; align-items: center; gap: 5px; color: #8a8a91; font-size: 7px; }
  .session small i { width: 5px; height: 5px; border-radius: 50%; background: var(--indigo); box-shadow: 0 0 0 2px rgba(77,60,224,.12); }
  .task-pane { min-width: 0; display: flex; flex-direction: column; background: #fff; }
  .pane-heading { height: 42px; display: flex; align-items: center; justify-content: space-between; padding: 0 18px; border-bottom: 1px solid #f0f0ee; font-size: 10px; font-weight: 700; }
  .pane-tools { color: #aaa; letter-spacing: 2px; }
  .timeline { flex: 1; display: flex; flex-direction: column; gap: 12px; padding: 20px 24px 10px; }
  .user-task { border-left: 2px solid var(--indigo); background: #f7f6ff; padding: 9px 11px; color: #474553; font-size: 9px; }
  .work-step { display: flex; align-items: center; gap: 9px; border-bottom: 1px solid #f1f1ef; padding: 0 1px 10px; }
  .step-icon { width: 19px; height: 19px; display: grid; place-items: center; flex: none; border-radius: 6px; background: #e9f7f1; color: var(--green); font-size: 9px; font-weight: 800; }
  .step-icon.indigo { background: var(--indigo-soft); color: var(--indigo); }
  .work-step span:last-child { display: flex; flex-direction: column; }.work-step b { font-size: 8px; }.work-step small { color: #929298; font-size: 7px; }
  .answer-block { margin-top: 2px; }.answer-meta { display: flex; align-items: center; gap: 5px; margin-bottom: 8px; color: #8d8d93; font-size: 7px; }.answer-meta span { width: 5px; height: 5px; border-radius: 50%; background: var(--green); }
  .answer-block strong { display: block; margin-bottom: 7px; font-family: ui-serif, Georgia, serif; font-size: 14px; line-height: 1.3; letter-spacing: -.01em; }.answer-block p { color: #73737a; font-size: 8px; line-height: 1.55; }
  .composer { height: 42px; display: flex; align-items: center; justify-content: space-between; margin: 0 18px 15px; border: 1px solid #dededc; border-radius: 11px; padding: 0 8px 0 12px; color: #aaa; font-size: 8px; box-shadow: 0 4px 12px rgba(30,30,36,.04); }
  .composer b { width: 23px; height: 23px; display: grid; place-items: center; border-radius: 7px; background: var(--ink); color: #fff; font-size: 10px; }
  .proof-strip { border-block: 1px solid var(--line); background: rgba(255,255,255,.55); }
  .proof-grid { display: grid; grid-template-columns: repeat(3, 1fr); }
  .proof-grid div { display: flex; flex-direction: column; gap: 2px; padding: 26px 34px; border-left: 1px solid var(--line); }.proof-grid div:last-child { border-right: 1px solid var(--line); }
  .proof-grid strong { font-size: 14px; }.proof-grid span { color: var(--muted); font-size: 12px; }
  .section { padding-block: 128px; }
  .section-heading { max-width: 720px; margin-bottom: 56px; }
  .overline { margin-bottom: 15px; color: var(--indigo); font-size: 12px; font-weight: 800; letter-spacing: .12em; text-transform: uppercase; }
  .section h2, .cta-section h2 { margin-bottom: 20px; font-size: clamp(38px, 5vw, 62px); line-height: 1.03; letter-spacing: -.055em; font-weight: 630; }
  .section-heading > p:last-child, .connection-copy > p:not(.overline) { color: var(--muted); font-size: 18px; }
  .feature-grid { display: grid; grid-template-columns: 1.2fr .8fr; gap: 18px; }
  .feature-card { min-width: 0; overflow: hidden; border: 1px solid var(--line); border-radius: var(--radius); background: rgba(255,255,255,.68); padding: 20px 26px 28px; }
  .feature-card.large { grid-row: span 2; }
  .feature-visual { position: relative; overflow: hidden; height: 190px; margin: -4px -10px 24px; border-radius: 14px; background: #efefed; }
  .large .feature-visual { height: 420px; }
  .card-number { margin-bottom: 9px; color: #9a9a9f; font-size: 10px; font-weight: 800; letter-spacing: .12em; }
  .feature-card h3 { margin-bottom: 8px; font-size: 22px; line-height: 1.2; letter-spacing: -.025em; }
  .feature-card > p:last-child { margin-bottom: 0; color: var(--muted); font-size: 14px; }
  .sessions-visual { background: linear-gradient(135deg, #26252c, #363540); }
  .mini-rail { position: absolute; inset: 24px auto 24px 24px; width: 30%; display: flex; flex-direction: column; gap: 11px; border-radius: 11px 0 0 11px; background: #313039; padding: 26px 15px; }.mini-rail i { display: block; height: 21px; border-radius: 6px; background: rgba(255,255,255,.08); }.mini-rail i:nth-child(2) { background: rgba(114,99,255,.38); }
  .mini-pane { position: absolute; inset: 24px 24px calc(50% + 3px) 32%; display: flex; flex-direction: column; gap: 11px; border: 1px solid rgba(255,255,255,.06); border-radius: 0 11px 0 0; background: #222127; padding: 30px 26px; }.mini-pane.second { inset: calc(50% + 3px) 24px 24px 32%; border-radius: 0 0 11px; }.mini-pane span { display: block; height: 8px; border-radius: 999px; background: rgba(255,255,255,.13); }.mini-pane span:nth-child(1) { width: 42%; background: rgba(114,99,255,.62); }.mini-pane span:nth-child(2) { width: 86%; }.mini-pane span:nth-child(3) { width: 68%; }
  .local-visual { display: grid; place-items: center; background: radial-gradient(circle, #f8f7ff, #e8e6f4); }.device { position: relative; z-index: 2; width: 94px; height: 70px; border: 5px solid #2a2930; border-radius: 10px; background: #fafafa; box-shadow: 0 14px 30px rgba(50,45,85,.16); }.device:after { content: ""; position: absolute; width: 32px; height: 5px; left: 26px; bottom: -14px; border-radius: 4px; background: #2a2930; }.device span { position: absolute; inset: 12px; border-radius: 5px; background: linear-gradient(135deg, #6c5bf0, #3e2ed3); }.device i { position: absolute; z-index: 2; width: 11px; height: 11px; left: 37px; top: 27px; border: 2px solid #fff; border-radius: 50%; }.orbit { position: absolute; border: 1px solid rgba(77,60,224,.18); border-radius: 50%; }.orbit-a { width: 150px; height: 150px; }.orbit-b { width: 210px; height: 210px; }
  .connect-visual { background: radial-gradient(circle at center, #fff, #eeedeb); }.connect-node { position: absolute; width: 43px; height: 43px; display: grid; place-items: center; border: 1px solid rgba(30,30,35,.1); border-radius: 13px; background: #fff; font-size: 11px; font-weight: 800; box-shadow: 0 8px 22px rgba(30,30,40,.08); }.connect-node:after { content: ""; position: absolute; z-index: -1; height: 1px; width: 90px; background: rgba(77,60,224,.18); transform-origin: center; }.connect-node.center { width: 62px; height: 62px; left: 50%; top: 50%; transform: translate(-50%,-50%); border-radius: 17px; }.connect-node.center:after { display: none; }.connect-node img { width: 46px; height: 46px; border-radius: 13px; }.node-a { left: 14%; top: 18%; color: #4285f4; }.node-a:after { left: 38px; top: 49px; transform: rotate(25deg); }.node-b { right: 12%; top: 26%; }.node-b:after { right: 36px; top: 48px; transform: rotate(-20deg); }.node-c { right: 22%; bottom: 10%; color: var(--indigo); }.node-c:after { right: 33px; bottom: 42px; transform: rotate(25deg); }
  .connect-section { display: grid; grid-template-columns: .9fr 1.1fr; gap: 90px; align-items: center; border-top: 1px solid var(--line); }
  .text-link { display: inline-flex; gap: 8px; color: var(--indigo-deep); font-size: 14px; font-weight: 700; }
  .permission-card { border: 1px solid var(--line); border-radius: var(--radius); background: #fff; padding: 28px; box-shadow: 0 24px 60px rgba(34,30,65,.1); }
  .permission-top { display: flex; align-items: center; gap: 12px; padding-bottom: 22px; border-bottom: 1px solid var(--line); }.permission-top > div { display: flex; flex: 1; flex-direction: column; }.permission-top b { font-size: 14px; }.permission-top small { color: var(--muted); font-size: 11px; }.google-mark { width: 42px; height: 42px; display: grid; place-items: center; border: 1px solid var(--line); border-radius: 12px; color: #4285f4; font-size: 18px; font-weight: 800; }.readonly { border-radius: 999px; background: #eaf6f1; padding: 5px 9px; color: #167454; font-size: 9px; font-weight: 850; letter-spacing: .08em; }
  .permission-list > div { display: flex; align-items: center; gap: 12px; padding: 18px 2px; border-bottom: 1px solid var(--line); }.permission-list > div > span { width: 23px; height: 23px; display: grid; place-items: center; flex: none; border-radius: 50%; background: #eaf6f1; color: var(--green); font-size: 11px; font-weight: 800; }.permission-list p { display: flex; flex-direction: column; margin: 0; }.permission-list b { font-size: 13px; }.permission-list small { color: var(--muted); font-size: 11px; }.permission-list .blocked > span { background: #f0f0ee; color: #9b9b9f; }.permission-list .blocked b { color: #77777d; text-decoration: line-through; }.permission-foot { margin: 18px 0 0; color: #7c7c83; font-size: 11px; }
  .cta-section { display: flex; align-items: flex-end; justify-content: space-between; gap: 30px; margin-bottom: 96px; border-radius: 30px; background: linear-gradient(135deg, #3c2ec6, #5d4cec); padding: 64px; color: #fff; box-shadow: 0 28px 70px rgba(70,52,205,.2); }.cta-section h2 { margin-bottom: 12px; }.cta-section p:last-child { margin-bottom: 0; color: rgba(255,255,255,.72); }.overline.light { color: rgba(255,255,255,.62); }.light-button { flex: none; background: #fff; color: #302782; }
  .site-footer { border-top: 1px solid var(--line); background: #efefed; }.footer-grid { display: grid; grid-template-columns: 1fr 1.4fr; gap: 60px; padding-block: 60px; }.footer-brand p { margin: 12px 0 0; color: var(--muted); font-size: 13px; }.footer-links { display: grid; grid-template-columns: repeat(3, 1fr); gap: 30px; }.footer-links div { display: flex; flex-direction: column; gap: 8px; }.footer-links b { margin-bottom: 5px; font-size: 12px; }.footer-links a { width: max-content; color: var(--muted); font-size: 12px; text-decoration: none; }.footer-links a:hover { color: var(--ink); }.footer-bottom { display: flex; justify-content: space-between; border-top: 1px solid var(--line); padding-block: 20px; color: #85858b; font-size: 11px; }
  .legal-main { max-width: 900px; padding-block: 92px 120px; }.legal-hero { padding-bottom: 52px; border-bottom: 1px solid var(--line); }.legal-hero h1 { margin-bottom: 22px; font-size: clamp(48px, 7vw, 78px); line-height: 1; letter-spacing: -.06em; font-weight: 640; }.legal-hero > p:not(.overline) { max-width: 720px; color: var(--muted); font-size: 18px; }.updated { margin-top: 28px; color: #8a8a90; font-size: 12px; }.legal-content { max-width: 760px; padding-top: 28px; }.legal-content section { scroll-margin-top: 30px; padding-block: 34px; border-bottom: 1px solid var(--line); }.legal-content h2 { margin-bottom: 16px; font-size: 25px; line-height: 1.2; letter-spacing: -.025em; }.legal-content h3 { margin: 24px 0 7px; font-size: 15px; }.legal-content p, .legal-content li { color: #55555d; font-size: 15px; }.legal-content p:last-child { margin-bottom: 0; }.legal-content a { color: var(--indigo-deep); }.legal-content ul, .legal-content ol { margin: 14px 0; padding-left: 24px; }.legal-content li + li { margin-top: 8px; }.legal-content code { border: 1px solid var(--line); border-radius: 5px; background: #ececea; padding: 2px 5px; color: #3c3c44; font-size: .87em; }
  .reveal { animation: rise .7s cubic-bezier(.22,.8,.3,1) both; }.reveal-delay { animation-delay: .12s; }@keyframes rise { from { opacity: 0; transform: translateY(18px); } to { opacity: 1; transform: translateY(0); } }
  :focus-visible { outline: 3px solid rgba(77,60,224,.35); outline-offset: 3px; }
  @media (max-width: 900px) {
    .shell { width: min(100% - 32px, 720px); }.nav { grid-template-columns: 1fr auto; }.nav-links { display: none; }.hero { min-height: auto; grid-template-columns: 1fr; gap: 56px; padding-block: 68px 76px; }.hero h1 { max-width: 680px; }.app-window { transform: none; }.feature-grid { grid-template-columns: 1fr; }.feature-card.large { grid-row: auto; }.large .feature-visual { height: 350px; }.connect-section { grid-template-columns: 1fr; gap: 50px; }.proof-grid div { padding-inline: 20px; }.cta-section { align-items: flex-start; flex-direction: column; }.footer-grid { grid-template-columns: 1fr; }
  }
  @media (max-width: 600px) {
    .shell { width: min(100% - 28px, 520px); }.site-header { background: var(--paper); }.nav { min-height: 66px; }.nav-download { padding: 8px 13px; }.hero { padding-block: 52px 62px; }.hero h1 { font-size: 46px; }.hero-lede { font-size: 16px; }.product-stage { margin-inline: -7px; }.app-window { aspect-ratio: .94; }.app-body { grid-template-columns: 1fr; }.session-rail { display: none; }.proof-grid { grid-template-columns: 1fr; }.proof-grid div, .proof-grid div:last-child { border-right: 1px solid var(--line); border-bottom: 1px solid var(--line); }.section { padding-block: 86px; }.section-heading { margin-bottom: 36px; }.section h2, .cta-section h2 { font-size: 39px; }.feature-card { padding-inline: 20px; }.large .feature-visual { height: 280px; }.mini-rail { display: none; }.mini-pane, .mini-pane.second { left: 24px; }.connect-section { gap: 36px; }.permission-card { padding: 21px; }.readonly { display: none; }.cta-section { width: calc(100% - 28px); margin-bottom: 60px; padding: 38px 28px; border-radius: 22px; }.footer-grid { padding-block: 48px; }.footer-links { grid-template-columns: 1fr 1fr; }.footer-bottom { gap: 10px; flex-direction: column; }.legal-main { padding-block: 66px 88px; }.legal-hero h1 { font-size: 50px; }
  }
  @media (prefers-reduced-motion: reduce) { html { scroll-behavior: auto; }.reveal { animation: none; }.button, .nav-download { transition: none; } }
`
