import { useParams, Link } from 'react-router-dom'
import { useState } from 'react'

// ── Copy button for code blocks ──────────────────────────────────────────────

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <button
      onClick={() => { navigator.clipboard.writeText(text); setCopied(true); setTimeout(() => setCopied(false), 2000) }}
      className="absolute top-2 right-2 px-2 py-1 text-[10px] font-medium rounded bg-gray-700 hover:bg-gray-600 text-gray-300 transition-colors cursor-pointer"
    >
      {copied ? 'Copied!' : 'Copy'}
    </button>
  )
}

function Code({ children }: { children: string }) {
  return (
    <div className="relative group my-3">
      <CopyButton text={children.trim()} />
      <pre className="bg-gray-800 border border-gray-700 rounded-lg p-4 pr-20 overflow-x-auto scrollbar-thin text-sm text-emerald-400 font-mono whitespace-pre">
        {children.trim()}
      </pre>
    </div>
  )
}

function InlineCode({ children }: { children: string }) {
  return <code className="px-1.5 py-0.5 bg-gray-800 border border-gray-700 rounded text-sm text-emerald-400 font-mono">{children}</code>
}

// ── Content sections ─────────────────────────────────────────────────────────

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mb-8">
      <h2 className="text-lg font-semibold text-white mb-3 flex items-center gap-2">
        {title}
      </h2>
      <div className="text-sm text-gray-300 leading-relaxed space-y-3">
        {children}
      </div>
    </div>
  )
}

function Warning({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex gap-3 p-3 bg-amber-900/20 border border-amber-800/40 rounded-lg text-sm text-amber-300">
      <svg className="w-5 h-5 flex-shrink-0 mt-0.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
        <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.082 16.5c-.77.833.192 2.5 1.732 2.5z" />
      </svg>
      <div>{children}</div>
    </div>
  )
}

// ── Finding content definitions ──────────────────────────────────────────────

interface FindingContent {
  title: string
  severity: 'critical' | 'warning' | 'info'
  category: string
  content: React.ReactNode
}

const findings: Record<string, FindingContent> = {

  // ── Access Control ───────────────────────────────────────────────────────

  'fw-active': {
    title: 'Firewall Enabled',
    severity: 'critical',
    category: 'Access Control',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            A firewall is your first line of defense against unauthorized network access. Without one, every service listening on your machine is directly reachable by anyone who can route to it &mdash; including services that were never meant to be public (databases, admin panels, debug endpoints).
          </p>
          <p>
            Even on a private LAN, a compromised device can laterally scan and attack other machines. A firewall ensures only explicitly allowed traffic reaches your services.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt checks whether a host-based firewall is active by querying the status of <InlineCode>ufw</InlineCode> (Uncomplicated Firewall) and <InlineCode>firewalld</InlineCode>. If neither reports an active/enabled state, this check fails.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Ubuntu / Debian (UFW):</strong></p>
          <Code>{`# Enable UFW with sensible defaults
sudo ufw default deny incoming
sudo ufw default allow outgoing

# Allow SSH so you don't lock yourself out
sudo ufw allow ssh

# Allow any other services you need (e.g. HTTP, HTTPS)
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp

# Enable the firewall
sudo ufw enable

# Verify status
sudo ufw status verbose`}</Code>

          <p><strong>CentOS / RHEL / Fedora (firewalld):</strong></p>
          <Code>{`# Start and enable firewalld
sudo systemctl enable --now firewalld

# Allow services
sudo firewall-cmd --permanent --add-service=ssh
sudo firewall-cmd --permanent --add-service=http
sudo firewall-cmd --permanent --add-service=https

# Reload to apply
sudo firewall-cmd --reload

# Verify
sudo firewall-cmd --list-all`}</Code>

          <Warning>
            Always ensure SSH access is allowed <strong>before</strong> enabling the firewall, especially on remote machines. Locking yourself out of a headless server is a very bad day.
          </Warning>
        </Section>
      </>
    ),
  },

  'mac-enabled': {
    title: 'Mandatory Access Control',
    severity: 'warning',
    category: 'Access Control',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            Mandatory Access Control (MAC) systems like <strong>SELinux</strong> and <strong>AppArmor</strong> add a layer of security beyond traditional Unix permissions. They confine processes to only the resources they need &mdash; so even if a service is compromised, the attacker can't access files or network resources outside that service's policy.
          </p>
          <p>
            Without MAC, a compromised web server process running as <InlineCode>www-data</InlineCode> can read anything that user can. With AppArmor or SELinux, it's confined to its declared profile regardless of Unix permissions.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt checks whether SELinux is in <InlineCode>enforcing</InlineCode> mode or AppArmor reports <InlineCode>enabled</InlineCode>/<InlineCode>active</InlineCode>. Permissive mode or disabled states are flagged.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Ubuntu / Debian (AppArmor):</strong></p>
          <p>AppArmor is usually installed by default on Ubuntu. Verify and enable it:</p>
          <Code>{`# Check current status
sudo aa-status

# If not installed
sudo apt install apparmor apparmor-utils

# Ensure it's enabled at boot
sudo systemctl enable apparmor

# List enforced profiles
sudo aa-status --enforced`}</Code>

          <p><strong>CentOS / RHEL / Fedora (SELinux):</strong></p>
          <p>SELinux is usually installed by default. If it was disabled:</p>
          <Code>{`# Check current status
getenforce

# If it says "Disabled" or "Permissive", set to enforcing:
sudo setenforce 1

# Make it permanent (survives reboot)
sudo sed -i 's/^SELINUX=.*/SELINUX=enforcing/' /etc/selinux/config`}</Code>

          <Warning>
            Switching SELinux from disabled to enforcing can cause services to fail if they weren't running with proper contexts. Test in permissive mode first (<InlineCode>setenforce 0</InlineCode>) and check <InlineCode>audit.log</InlineCode> for denials before enforcing.
          </Warning>
        </Section>
      </>
    ),
  },

  'failed-logins-low': {
    title: 'Failed Login Attempts',
    severity: 'warning',
    category: 'Access Control',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            A high number of failed login attempts usually indicates brute-force SSH attacks or credential stuffing. While a few failed attempts are normal (typos, old SSH keys), sustained high counts mean someone is actively trying to gain access.
          </p>
          <p>
            Even if your passwords are strong, brute-force attempts consume resources and fill logs. An automated response system like fail2ban drastically reduces the attack surface.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt counts failed authentication attempts over the last 24 hours from <InlineCode>journalctl</InlineCode> (sshd unit) or <InlineCode>/var/log/auth.log</InlineCode>. Fewer than 10 failures passes; 10&ndash;49 is a warning; 50+ is critical.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Install fail2ban</strong> to automatically ban IPs after repeated failures:</p>
          <Code>{`# Install
sudo apt install fail2ban    # Debian/Ubuntu
sudo dnf install fail2ban    # Fedora/RHEL

# Enable and start
sudo systemctl enable --now fail2ban

# Check jail status
sudo fail2ban-client status sshd`}</Code>

          <p><strong>Harden SSH</strong> to eliminate password attacks entirely:</p>
          <Code>{`# Edit SSH config
sudo nano /etc/ssh/sshd_config

# Disable password authentication (key-only)
PasswordAuthentication no

# Disable root login
PermitRootLogin no

# Restart SSH
sudo systemctl restart sshd`}</Code>

          <Warning>
            Before disabling password auth, make sure you have a working SSH key pair installed. Test key-based login in a <strong>separate session</strong> before closing your current one.
          </Warning>

          <p><strong>Investigate current failures:</strong></p>
          <Code>{`# View recent failed attempts
sudo journalctl -u sshd --since "24 hours ago" | grep "Failed"

# See which IPs are trying
sudo journalctl -u sshd --since "24 hours ago" | grep "Failed" | awk '{print $(NF-3)}' | sort | uniq -c | sort -rn | head -20`}</Code>
        </Section>
      </>
    ),
  },

  'logged-in-users': {
    title: 'Active User Sessions',
    severity: 'warning',
    category: 'Access Control',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            More active sessions than expected could indicate unauthorized access. On a typical server, you'd expect 0&ndash;2 sessions (your own SSH session, maybe a cron job). If you see sessions you don't recognize, someone else may have access.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt runs the <InlineCode>who</InlineCode> command and counts active sessions. Two or fewer passes; more than two triggers a warning.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Review active sessions:</strong></p>
          <Code>{`# See who is logged in, from where
who

# More detailed view with process info
w

# See login history
last -20`}</Code>

          <p><strong>Terminate suspicious sessions:</strong></p>
          <Code>{`# Find the TTY of the suspicious session from 'who' output, then:
sudo pkill -KILL -t pts/3    # Replace pts/3 with the actual TTY`}</Code>

          <p>If you see sessions from unexpected IPs, investigate immediately &mdash; check SSH keys in <InlineCode>~/.ssh/authorized_keys</InlineCode> for all users.</p>
        </Section>
      </>
    ),
  },

  // ── Patching ─────────────────────────────────────────────────────────────

  'no-security-updates': {
    title: 'Pending Security Updates',
    severity: 'critical',
    category: 'Patching',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            Security updates patch known vulnerabilities &mdash; bugs that attackers already know about and may have exploit code for. Every day a security update goes unapplied, your system is exposed to a publicly documented attack path.
          </p>
          <p>
            Security updates are specifically marked by distributions because they fix CVEs (Common Vulnerabilities and Exposures). These are the highest-priority patches on any system.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt queries the package manager for updates flagged as security-related. On Debian/Ubuntu this uses <InlineCode>apt list --upgradable</InlineCode> with security archive matching; on RHEL/Fedora it uses <InlineCode>dnf check-update --security</InlineCode>.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Debian / Ubuntu:</strong></p>
          <Code>{`# Apply security updates only
sudo apt update
sudo apt upgrade -y --only-upgrade -o Dir::Etc::SourceList=/etc/apt/sources.list.d/ubuntu-security.list

# Or apply all updates (includes security)
sudo apt update && sudo apt upgrade -y`}</Code>

          <p><strong>CentOS / RHEL / Fedora:</strong></p>
          <Code>{`# Apply security updates only
sudo dnf update --security -y

# Apply all updates
sudo dnf update -y`}</Code>

          <p>
            You can also use the <strong>Patch Now</strong> button in the security score modal to trigger this remotely via rIOt.
          </p>

          <Warning>
            Some security updates (especially kernel updates) require a reboot to take effect. Plan a maintenance window for production systems.
          </Warning>
        </Section>
      </>
    ),
  },

  'pending-updates-low': {
    title: 'Pending Package Updates',
    severity: 'warning',
    category: 'Patching',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            While not all pending updates are security-critical, falling far behind on updates creates risk. Bug fixes, performance improvements, and compatibility patches accumulate. The larger the gap, the more disruptive the eventual update becomes &mdash; and the more likely you are to hit a known bug.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt counts total pending package updates. Five or fewer is considered healthy; more than five triggers a warning, indicating the system is falling behind.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Debian / Ubuntu:</strong></p>
          <Code>{`# See what's pending
apt list --upgradable

# Apply all updates
sudo apt update && sudo apt upgrade -y

# If held packages need upgrading
sudo apt dist-upgrade -y`}</Code>

          <p><strong>CentOS / RHEL / Fedora:</strong></p>
          <Code>{`# See what's pending
dnf check-update

# Apply all updates
sudo dnf update -y`}</Code>

          <p>
            Consider setting up a regular maintenance schedule (weekly or monthly) to keep systems current without surprise.
          </p>
        </Section>
      </>
    ),
  },

  'no-kernel-update': {
    title: 'Kernel Update Pending',
    severity: 'warning',
    category: 'Patching',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            Kernel vulnerabilities are among the most severe because the kernel runs with the highest privilege level. A kernel exploit can bypass all userspace security controls &mdash; containers, AppArmor, firewalls, everything.
          </p>
          <p>
            Unlike userspace packages, kernel updates require a <strong>reboot</strong> to take effect. The update may be installed, but if you haven't rebooted, you're still running the old vulnerable kernel.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt checks if the package manager reports a pending kernel update. This means a newer kernel package is available but either not installed or not yet booted into.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Install the update and reboot:</strong></p>
          <Code>{`# Debian / Ubuntu
sudo apt update && sudo apt upgrade -y
sudo reboot

# RHEL / Fedora
sudo dnf update -y
sudo reboot`}</Code>

          <p><strong>Check which kernel you're running vs. installed:</strong></p>
          <Code>{`# Currently running kernel
uname -r

# Installed kernel packages (Debian/Ubuntu)
dpkg -l | grep linux-image

# Installed kernel packages (RHEL/Fedora)
rpm -qa | grep kernel-core`}</Code>

          <p>
            On production systems, you can schedule the reboot via rIOt's <strong>Reboot</strong> command during a maintenance window.
          </p>
        </Section>
      </>
    ),
  },

  'auto-updates': {
    title: 'Automatic Security Updates',
    severity: 'info',
    category: 'Patching',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            Manual patching doesn't scale. Even with the best intentions, humans forget, get busy, or deprioritize updates. Automatic security updates ensure critical patches are applied within hours of release, not days or weeks.
          </p>
          <p>
            Most automatic update tools only apply security patches by default &mdash; they won't surprise you with breaking changes from major version bumps.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt checks whether <InlineCode>unattended-upgrades</InlineCode> (Debian/Ubuntu) or equivalent is configured and enabled.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Debian / Ubuntu (unattended-upgrades):</strong></p>
          <Code>{`# Install
sudo apt install unattended-upgrades

# Enable
sudo dpkg-reconfigure -plow unattended-upgrades

# Verify it's configured
cat /etc/apt/apt.conf.d/20auto-upgrades`}</Code>
          <p>Expected content:</p>
          <Code>{`APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";`}</Code>

          <p><strong>RHEL / Fedora (dnf-automatic):</strong></p>
          <Code>{`# Install
sudo dnf install dnf-automatic

# Configure for security updates only
sudo sed -i 's/apply_updates = no/apply_updates = yes/' /etc/dnf/automatic.conf
sudo sed -i 's/upgrade_type = default/upgrade_type = security/' /etc/dnf/automatic.conf

# Enable the timer
sudo systemctl enable --now dnf-automatic.timer`}</Code>

          <p>
            You can also use the <strong>Enable</strong> button in the security score modal to set this up remotely via rIOt.
          </p>
        </Section>
      </>
    ),
  },

  // ── Network ──────────────────────────────────────────────────────────────

  'minimal-open-ports': {
    title: 'Open Port Count',
    severity: 'warning',
    category: 'Network',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            Every open port is a potential entry point. Each listening service increases your attack surface &mdash; more services means more code that could have vulnerabilities, more things to patch, and more things to monitor.
          </p>
          <p>
            The principle of <strong>least exposure</strong>: only run the services you need, and only expose them to the networks that need to reach them.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt counts the number of TCP ports in LISTEN state on the device. Five or fewer is considered minimal; more triggers a review warning.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Identify what's listening:</strong></p>
          <Code>{`# Show all listening ports with process names
sudo ss -tlnp

# Alternative
sudo netstat -tlnp`}</Code>

          <p><strong>Disable unnecessary services:</strong></p>
          <Code>{`# Stop and disable a service
sudo systemctl stop <service-name>
sudo systemctl disable <service-name>

# Example: disable a test web server you forgot about
sudo systemctl stop apache2
sudo systemctl disable apache2`}</Code>

          <p><strong>Bind services to localhost</strong> if they only need local access:</p>
          <Code>{`# In the service's config, bind to 127.0.0.1 instead of 0.0.0.0
# Example for PostgreSQL (pg_hba.conf / postgresql.conf):
listen_addresses = 'localhost'`}</Code>
        </Section>
      </>
    ),
  },

  'no-risky-ports': {
    title: 'Insecure Service Ports',
    severity: 'critical',
    category: 'Network',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            Certain protocols are inherently insecure because they transmit data (including credentials) in plaintext:
          </p>
          <ul className="list-disc list-inside space-y-1 ml-2">
            <li><strong>FTP (port 21)</strong> &mdash; passwords sent in cleartext; use SFTP instead</li>
            <li><strong>Telnet (port 23)</strong> &mdash; everything in cleartext; use SSH instead</li>
            <li><strong>SMTP (port 25)</strong> &mdash; unencrypted email relay; potential spam relay</li>
            <li><strong>rsh (port 514)</strong> &mdash; remote shell with no encryption; use SSH</li>
          </ul>
          <p>
            Anyone on the same network can trivially capture credentials from these protocols with a packet sniffer.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt checks the open port list for ports 21 (FTP), 23 (Telnet), 25 (SMTP), and 514 (rsh). If any are listening, this check fails as critical.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Replace insecure services with secure alternatives:</strong></p>
          <Code>{`# Disable FTP, use SFTP (built into SSH) instead
sudo systemctl stop vsftpd
sudo systemctl disable vsftpd

# Disable Telnet
sudo systemctl stop telnet.socket
sudo systemctl disable telnet.socket

# For SMTP, ensure it's only listening locally or uses STARTTLS
# In Postfix main.cf:
inet_interfaces = loopback-only`}</Code>

          <p><strong>If you must run one of these services</strong>, restrict access via firewall:</p>
          <Code>{`# Allow FTP only from a specific trusted IP
sudo ufw allow from 192.168.1.100 to any port 21`}</Code>
        </Section>
      </>
    ),
  },

  'tls-certs-valid': {
    title: 'TLS Certificate Status',
    severity: 'critical',
    category: 'Network',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            Expired TLS certificates cause immediate, visible failures &mdash; browsers show scary warnings, API clients refuse to connect, and automated systems break. Beyond availability, expired certs may cause clients to fall back to unencrypted connections.
          </p>
          <p>
            Certificates expiring within 30 days need attention now. Renewal often requires DNS validation or HTTP challenges that can fail unexpectedly.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt parses TLS certificates found in web server configurations (Nginx, Caddy) and checks their expiration dates. Certificates with fewer than 30 days remaining or already expired are flagged.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Using Let's Encrypt with Certbot (most common):</strong></p>
          <Code>{`# Install certbot
sudo apt install certbot python3-certbot-nginx   # For Nginx
sudo apt install certbot python3-certbot-apache  # For Apache

# Issue/renew certificate
sudo certbot --nginx -d yourdomain.com

# Test auto-renewal
sudo certbot renew --dry-run

# Check certificate expiration
sudo certbot certificates`}</Code>

          <p><strong>Caddy</strong> handles TLS automatically &mdash; if certs are expiring, check:</p>
          <Code>{`# Caddy auto-manages certs. If they're failing, check:
sudo journalctl -u caddy --since "1 hour ago" | grep -i "tls\|cert\|acme"

# Verify Caddy can reach ACME endpoints
curl -I https://acme-v02.api.letsencrypt.org/directory`}</Code>

          <p><strong>Check a certificate manually:</strong></p>
          <Code>{`# View certificate details
openssl x509 -in /path/to/cert.pem -noout -dates -subject

# Check a remote endpoint
echo | openssl s_client -connect yourdomain.com:443 2>/dev/null | openssl x509 -noout -dates`}</Code>
        </Section>
      </>
    ),
  },

  'proxy-config-valid': {
    title: 'Web Server Configuration',
    severity: 'warning',
    category: 'Network',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            A web server with configuration errors may silently fail to apply security settings, serve incorrect content, or refuse to restart after an update. Config validation catches syntax errors and directive conflicts before they cause downtime.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt runs the web server's built-in config test (<InlineCode>nginx -t</InlineCode> or Caddy's config validation) and reports whether the configuration is valid.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Nginx:</strong></p>
          <Code>{`# Test configuration (shows error location)
sudo nginx -t

# If it reports an error, it'll show the file and line number.
# Fix the error, then test again:
sudo nginx -t

# Reload after fixing
sudo systemctl reload nginx`}</Code>

          <p><strong>Caddy:</strong></p>
          <Code>{`# Validate config
caddy validate --config /etc/caddy/Caddyfile

# Reload after fixing
sudo systemctl reload caddy`}</Code>
        </Section>
      </>
    ),
  },

  'security-headers': {
    title: 'Security Headers Configured',
    severity: 'info',
    category: 'Network',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            HTTP security headers instruct browsers to enable built-in security features. Without them, your users are more vulnerable to:
          </p>
          <ul className="list-disc list-inside space-y-1 ml-2">
            <li><strong>Clickjacking</strong> &mdash; your site embedded in a malicious iframe</li>
            <li><strong>MIME sniffing</strong> &mdash; browser reinterpreting content types</li>
            <li><strong>Protocol downgrade</strong> &mdash; HTTPS connections falling back to HTTP</li>
            <li><strong>XSS</strong> &mdash; injected scripts executing in your domain's context</li>
          </ul>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt counts the security-related headers configured in your web server. Having at least 3 of the key headers (HSTS, X-Frame-Options, X-Content-Type-Options, CSP, etc.) passes the check.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Nginx &mdash;</strong> add to your <InlineCode>server</InlineCode> block:</p>
          <Code>{`# Prevent clickjacking
add_header X-Frame-Options "SAMEORIGIN" always;

# Prevent MIME type sniffing
add_header X-Content-Type-Options "nosniff" always;

# Enable HSTS (force HTTPS for 1 year)
add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

# Basic XSS protection
add_header X-XSS-Protection "1; mode=block" always;

# Referrer policy
add_header Referrer-Policy "strict-origin-when-cross-origin" always;

# Content Security Policy (adjust to your needs)
add_header Content-Security-Policy "default-src 'self'" always;`}</Code>

          <p><strong>Caddy &mdash;</strong> add to your site block:</p>
          <Code>{`header {
    X-Frame-Options "SAMEORIGIN"
    X-Content-Type-Options "nosniff"
    Strict-Transport-Security "max-age=31536000; includeSubDomains"
    X-XSS-Protection "1; mode=block"
    Referrer-Policy "strict-origin-when-cross-origin"
}`}</Code>

          <p><strong>Verify your headers:</strong></p>
          <Code>{`curl -I https://yourdomain.com | grep -iE "x-frame|x-content|strict-transport|x-xss|referrer-policy|content-security"`}</Code>
        </Section>
      </>
    ),
  },

  'rate-limiting': {
    title: 'Rate Limiting Configured',
    severity: 'info',
    category: 'Network',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            Rate limiting protects your services from abuse &mdash; brute-force login attempts, API scraping, denial-of-service, and credential stuffing. Without it, an attacker can make unlimited requests at whatever speed their connection allows.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt checks your web server configuration for rate limiting directives (<InlineCode>limit_req_zone</InlineCode> in Nginx, <InlineCode>rate_limit</InlineCode> in Caddy).
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Nginx:</strong></p>
          <Code>{`# In http block: define a rate limit zone
limit_req_zone $binary_remote_addr zone=general:10m rate=10r/s;

# In server/location block: apply it
location / {
    limit_req zone=general burst=20 nodelay;
}

# Stricter limit for login endpoints
limit_req_zone $binary_remote_addr zone=login:10m rate=5r/m;
location /login {
    limit_req zone=login burst=3 nodelay;
}`}</Code>

          <p><strong>Caddy:</strong></p>
          <Code>{`# Using the rate_limit directive (requires caddy-ratelimit plugin)
# Or use Caddy's built-in request matchers with respond
rate_limit {
    zone dynamic_zone {
        key {remote_host}
        events 10
        window 1s
    }
}`}</Code>
        </Section>
      </>
    ),
  },

  // ── Docker ───────────────────────────────────────────────────────────────

  'container-restart-policy': {
    title: 'Container Restart Policies',
    severity: 'warning',
    category: 'Docker',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            Without a restart policy, a crashed container stays down until someone manually restarts it. For production services, this means unplanned downtime that could last minutes, hours, or until someone notices.
          </p>
          <p>
            A restart policy is the simplest form of self-healing &mdash; if a process crashes due to a transient error (OOM, segfault, dependency blip), it comes right back.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt checks every running container for a restart policy. Containers with no policy or <InlineCode>no</InlineCode> as their restart policy cause this check to fail.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Docker Compose (recommended):</strong></p>
          <Code>{`services:
  myapp:
    image: myapp:latest
    restart: unless-stopped   # Restarts on crash, but not if you manually stop it`}</Code>

          <p><strong>Docker CLI:</strong></p>
          <Code>{`# Update a running container's restart policy
docker update --restart unless-stopped <container_name>

# Or when creating
docker run -d --restart unless-stopped myapp:latest`}</Code>

          <p><strong>Available policies:</strong></p>
          <ul className="list-disc list-inside space-y-1 ml-2">
            <li><InlineCode>no</InlineCode> &mdash; never restart (default)</li>
            <li><InlineCode>on-failure</InlineCode> &mdash; restart only on non-zero exit code</li>
            <li><InlineCode>unless-stopped</InlineCode> &mdash; always restart unless explicitly stopped</li>
            <li><InlineCode>always</InlineCode> &mdash; always restart, even after manual stop + daemon restart</li>
          </ul>
          <p>
            For most services, <InlineCode>unless-stopped</InlineCode> is the best choice.
          </p>
        </Section>
      </>
    ),
  },

  'container-health-checks': {
    title: 'Container Health Checks',
    severity: 'info',
    category: 'Docker',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            A running container isn't necessarily a <em>healthy</em> container. The process might be up but deadlocked, unable to serve requests, or stuck in a crash loop. Docker health checks let you define what "healthy" means for your application.
          </p>
          <p>
            Health checks enable orchestrators to make informed decisions &mdash; routing traffic away from unhealthy containers, triggering restarts, or alerting operators.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt checks whether running containers have a health check defined. Containers reporting <InlineCode>none</InlineCode> or empty health status are flagged.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>In a Dockerfile:</strong></p>
          <Code>{`HEALTHCHECK --interval=30s --timeout=5s --retries=3 \\
  CMD curl -f http://localhost:8080/health || exit 1`}</Code>

          <p><strong>In Docker Compose:</strong></p>
          <Code>{`services:
  myapp:
    image: myapp:latest
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s`}</Code>

          <p><strong>For non-HTTP services</strong>, use appropriate checks:</p>
          <Code>{`# PostgreSQL
healthcheck:
  test: ["CMD-SHELL", "pg_isready -U postgres"]
  interval: 10s

# Redis
healthcheck:
  test: ["CMD", "redis-cli", "ping"]
  interval: 10s

# Generic TCP check
healthcheck:
  test: ["CMD-SHELL", "nc -z localhost 3000"]
  interval: 15s`}</Code>
        </Section>
      </>
    ),
  },

  'container-mem-limits': {
    title: 'Container Memory Limits',
    severity: 'warning',
    category: 'Docker',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            A container without memory limits can consume all available RAM on the host, triggering the kernel OOM killer. The OOM killer doesn't discriminate &mdash; it may kill critical processes, your database, or the Docker daemon itself.
          </p>
          <p>
            Memory limits are a safety net. They ensure a misbehaving container (memory leak, runaway cache) can only consume its allocated share and gets OOM-killed in isolation rather than taking down the host.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt checks whether running containers have a memory limit set. Containers with no limit (or 0) are flagged.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Docker Compose:</strong></p>
          <Code>{`services:
  myapp:
    image: myapp:latest
    deploy:
      resources:
        limits:
          memory: 512M     # Hard limit — container is OOM-killed if exceeded
        reservations:
          memory: 256M     # Soft limit — guaranteed minimum`}</Code>

          <p><strong>Docker CLI:</strong></p>
          <Code>{`# Set on a new container
docker run -d --memory 512m --memory-swap 1g myapp:latest

# Update a running container
docker update --memory 512m --memory-swap 1g <container_name>`}</Code>

          <p><strong>How to determine the right limit:</strong></p>
          <Code>{`# Check current memory usage of your containers
docker stats --no-stream --format "table {{.Name}}\t{{.MemUsage}}\t{{.MemPerc}}"

# Set the limit to 1.5-2x the normal usage to allow for spikes`}</Code>
        </Section>
      </>
    ),
  },

  'no-sensitive-mounts': {
    title: 'Sensitive Volume Mounts',
    severity: 'warning',
    category: 'Docker',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            Mounting the Docker socket (<InlineCode>/var/run/docker.sock</InlineCode>) into a container gives that container <strong>full control over the Docker daemon</strong> &mdash; equivalent to root access on the host. A compromised container with socket access can:
          </p>
          <ul className="list-disc list-inside space-y-1 ml-2">
            <li>Start privileged containers</li>
            <li>Mount the host filesystem</li>
            <li>Execute commands on the host</li>
            <li>Access secrets from other containers</li>
          </ul>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt checks running containers for <strong>writable</strong> bind mounts of <InlineCode>/var/run/docker.sock</InlineCode>. Read-only mounts (<InlineCode>:ro</InlineCode>) are not flagged.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Option 1: Mount read-only</strong> (if the container only needs to read container info):</p>
          <Code>{`# Docker Compose
volumes:
  - /var/run/docker.sock:/var/run/docker.sock:ro

# Docker CLI
docker run -v /var/run/docker.sock:/var/run/docker.sock:ro myapp`}</Code>

          <p><strong>Option 2: Use a Docker socket proxy</strong> (best security):</p>
          <Code>{`# Run a socket proxy that filters API calls
# Example with tecnativa/docker-socket-proxy:
services:
  docker-proxy:
    image: tecnativa/docker-socket-proxy
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      CONTAINERS: 1    # Allow container listing
      POST: 0          # Deny all write operations

  myapp:
    image: myapp:latest
    environment:
      DOCKER_HOST: tcp://docker-proxy:2375`}</Code>

          <Warning>
            Some tools (Traefik, Portainer, Watchtower) legitimately need Docker socket access. Evaluate whether read-only access or a socket proxy is sufficient for your use case.
          </Warning>
        </Section>
      </>
    ),
  },

  // ── System ───────────────────────────────────────────────────────────────

  'no-failed-services': {
    title: 'Failed Systemd Services',
    severity: 'warning',
    category: 'System',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            Failed systemd services indicate something is broken on your system. It could be a misconfigured service, a missing dependency, a permissions issue, or a service that crashed and couldn't restart. Failed services may include security-critical components like log rotation, monitoring agents, or backup jobs.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt lists all systemd services and counts those in a <InlineCode>failed</InlineCode> state. Any failed services trigger a warning.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Investigate failed services:</strong></p>
          <Code>{`# List all failed services
systemctl --failed

# Check why a specific service failed
systemctl status <service-name>
journalctl -u <service-name> --since "1 hour ago" -n 50`}</Code>

          <p><strong>Common fixes:</strong></p>
          <Code>{`# Restart a failed service
sudo systemctl restart <service-name>

# If it keeps failing, check config and logs
sudo systemctl status <service-name>
journalctl -u <service-name> -e

# Reset the failed state (after fixing the underlying issue)
sudo systemctl reset-failed <service-name>

# If the service is not needed, disable it
sudo systemctl disable <service-name>
sudo systemctl reset-failed <service-name>`}</Code>
        </Section>
      </>
    ),
  },

  'reasonable-uptime': {
    title: 'System Uptime',
    severity: 'info',
    category: 'System',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            While long uptime is often seen as a badge of honor, it actually means the system hasn't rebooted to apply kernel patches. Kernel updates &mdash; often the most critical security fixes &mdash; only take effect after a reboot.
          </p>
          <p>
            A system with 180+ days of uptime has almost certainly missed kernel-level security patches. The running kernel may have known exploitable vulnerabilities.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt flags systems with more than 180 days (6 months) of uptime as potentially missing kernel-level patches that require a reboot.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Check if a reboot is actually needed:</strong></p>
          <Code>{`# Check if running kernel matches the latest installed
uname -r                              # Running kernel
ls /boot/vmlinuz-* | sort -V | tail -1  # Latest installed

# Debian/Ubuntu: check if reboot is required
ls /var/run/reboot-required 2>/dev/null && echo "Reboot needed" || echo "No reboot needed"

# RHEL: compare kernel versions
needs-restarting -r`}</Code>

          <p><strong>Schedule a reboot:</strong></p>
          <Code>{`# Reboot now
sudo reboot

# Schedule a reboot (e.g., at 3 AM)
sudo shutdown -r 03:00

# Or use rIOt's Reboot command from the device detail page`}</Code>
        </Section>
      </>
    ),
  },

  'dns-configured': {
    title: 'DNS Configured',
    severity: 'warning',
    category: 'System',
    content: (
      <>
        <Section title="Why This Matters">
          <p>
            Without DNS, your system can't resolve hostnames &mdash; package updates fail, time sync breaks, API calls to external services fail, and TLS certificate validation may not work (can't check OCSP/CRL endpoints).
          </p>
          <p>
            DNS misconfiguration is a common cause of subtle, hard-to-diagnose failures that only appear when the system needs to reach the internet.
          </p>
        </Section>

        <Section title="What This Check Does">
          <p>
            rIOt checks whether the system has at least one DNS server configured by inspecting the network telemetry.
          </p>
        </Section>

        <Section title="How to Fix It">
          <p><strong>Check current DNS configuration:</strong></p>
          <Code>{`# systemd-resolved (modern distros)
resolvectl status

# Traditional resolv.conf
cat /etc/resolv.conf`}</Code>

          <p><strong>Configure DNS with systemd-resolved:</strong></p>
          <Code>{`# Edit resolved configuration
sudo nano /etc/systemd/resolved.conf

# Add DNS servers
[Resolve]
DNS=1.1.1.1 9.9.9.9
FallbackDNS=8.8.8.8 8.8.4.4

# Restart
sudo systemctl restart systemd-resolved`}</Code>

          <p><strong>Configure DNS directly (if not using systemd-resolved):</strong></p>
          <Code>{`# Edit resolv.conf
sudo nano /etc/resolv.conf

# Add nameservers
nameserver 1.1.1.1
nameserver 9.9.9.9`}</Code>

          <p><strong>Test DNS resolution:</strong></p>
          <Code>{`dig google.com
nslookup google.com
host google.com`}</Code>
        </Section>
      </>
    ),
  },
}

// ── Page component ───────────────────────────────────────────────────────────

function severityStyle(severity: string) {
  switch (severity) {
    case 'critical': return 'bg-red-500/20 text-red-400 border-red-500/30'
    case 'warning':  return 'bg-amber-500/20 text-amber-400 border-amber-500/30'
    default:         return 'bg-blue-500/20 text-blue-400 border-blue-500/30'
  }
}

export default function LearnMore() {
  const { findingId } = useParams<{ findingId: string }>()
  const finding = findingId ? findings[findingId] : undefined

  if (!finding) {
    return (
      <div className="max-w-3xl mx-auto py-12 px-6">
        <h1 className="text-2xl font-bold text-white mb-4">Article Not Found</h1>
        <p className="text-gray-400 mb-6">The requested security guide could not be found.</p>
        <Link to="/" className="text-blue-400 hover:text-blue-300 text-sm">&larr; Back to Fleet</Link>
      </div>
    )
  }

  return (
    <div className="max-w-3xl mx-auto py-8 px-6">
      {/* Header */}
      <div className="mb-8">
        <div className="flex items-center gap-3 mb-1 text-xs text-gray-500">
          <Link to="/" className="hover:text-gray-300 transition-colors">&larr; Back to Fleet</Link>
          <span>/</span>
          <span>Security Guide</span>
          <span>/</span>
          <span>{finding.category}</span>
        </div>
        <h1 className="text-2xl font-bold text-white mt-3 mb-2">{finding.title}</h1>
        <span className={`inline-block px-2 py-0.5 text-xs font-medium uppercase rounded border ${severityStyle(finding.severity)}`}>
          {finding.severity}
        </span>
      </div>

      {/* Content */}
      <div className="prose prose-invert max-w-none">
        {finding.content}
      </div>
    </div>
  )
}
