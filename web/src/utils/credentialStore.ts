/**
 * Encrypted credential storage using Web Crypto API (AES-GCM).
 * Stores an encrypted password in localStorage, keyed to the current origin.
 */

const STORAGE_KEY = 'riot_saved_cred'
const SALT_KEY = 'riot_cred_salt'

async function deriveKey(salt: Uint8Array): Promise<CryptoKey> {
  // Derive a key from the origin so the ciphertext is tied to this site
  const raw = new TextEncoder().encode(window.location.origin + ':riot-remember')
  const baseKey = await crypto.subtle.importKey('raw', raw as unknown as BufferSource, 'PBKDF2', false, ['deriveKey'])
  return crypto.subtle.deriveKey(
    { name: 'PBKDF2', salt: salt as unknown as BufferSource, iterations: 100_000, hash: 'SHA-256' },
    baseKey,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt'],
  )
}

export async function savePassword(password: string): Promise<void> {
  const salt = crypto.getRandomValues(new Uint8Array(16))
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const key = await deriveKey(salt)
  const ciphertext = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv },
    key,
    new TextEncoder().encode(password),
  )
  const payload = {
    s: btoa(String.fromCharCode(...salt)),
    iv: btoa(String.fromCharCode(...iv)),
    ct: btoa(String.fromCharCode(...new Uint8Array(ciphertext))),
  }
  localStorage.setItem(SALT_KEY, payload.s)
  localStorage.setItem(STORAGE_KEY, JSON.stringify({ iv: payload.iv, ct: payload.ct }))
}

export async function loadPassword(): Promise<string | null> {
  const saltB64 = localStorage.getItem(SALT_KEY)
  const raw = localStorage.getItem(STORAGE_KEY)
  if (!saltB64 || !raw) return null
  try {
    const { iv: ivB64, ct: ctB64 } = JSON.parse(raw)
    const salt = Uint8Array.from(atob(saltB64), c => c.charCodeAt(0))
    const iv = Uint8Array.from(atob(ivB64), c => c.charCodeAt(0))
    const ct = Uint8Array.from(atob(ctB64), c => c.charCodeAt(0))
    const key = await deriveKey(salt)
    const plain = await crypto.subtle.decrypt({ name: 'AES-GCM', iv }, key, ct)
    return new TextDecoder().decode(plain)
  } catch {
    clearPassword()
    return null
  }
}

export function clearPassword(): void {
  localStorage.removeItem(STORAGE_KEY)
  localStorage.removeItem(SALT_KEY)
}

export function hasStoredPassword(): boolean {
  return localStorage.getItem(STORAGE_KEY) !== null
}
