import { REDACTED_SECRET } from '../utils/secrets'

interface SecretInputProps {
  label: string
  /** Config key, used to build a unique, non-credential-looking field name. */
  name: string
  /** The config value as received from the server: the sentinel, '', or text typed by the user. */
  value: unknown
  /** True when the server holds a value for this secret (the channel was loaded with the sentinel). */
  configured: boolean
  onChange: (v: string) => void
  placeholder?: string
  /** Render as a password input. */
  masked?: boolean
  /** Offer an explicit Clear control for an optional secret. */
  clearable?: boolean
}

/**
 * A secret field that understands the write-only sentinel contract
 * (HARDEN-002 AD-009). The sentinel is never shown as editable text:
 *
 *  - untouched and configured: shows "Configured (unchanged)", submits the sentinel
 *  - typed over: submits the typed text
 *  - erased while configured: reverts to the sentinel, so a field cannot be
 *    wiped by accident
 *  - Clear (optional secrets only): submits '' and shows "Will be cleared on
 *    save" with an Undo
 *
 * Every input opts out of browser credential autofill and password saving, so
 * a saved rIOt login cannot be dropped into an empty secret field and
 * submitted as the notification credential.
 */
export default function SecretInput({
  label,
  name,
  value,
  configured,
  onChange,
  placeholder,
  masked = false,
  clearable = false,
}: SecretInputProps) {
  const isUnchanged = configured && value === REDACTED_SECRET
  const willClear = configured && value === ''
  const displayValue = typeof value === 'string' && value !== REDACTED_SECRET ? value : ''
  const fieldName = `riot-secret-${name}`

  return (
    <div>
      <label htmlFor={fieldName} className="block text-xs text-gray-400 mb-1">{label}</label>
      <input
        id={fieldName}
        name={fieldName}
        aria-label={label}
        type={masked ? 'password' : 'text'}
        autoComplete={masked ? 'new-password' : 'off'}
        data-1p-ignore="true"
        data-lpignore="true"
        data-bwignore="true"
        data-form-type="other"
        spellCheck={false}
        autoCapitalize="off"
        autoCorrect="off"
        value={displayValue}
        onChange={e => {
          const text = e.target.value
          onChange(text === '' && configured ? REDACTED_SECRET : text)
        }}
        placeholder={isUnchanged ? 'Leave blank to keep the current value' : placeholder}
        className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
      />
      {isUnchanged && <p className="text-xs text-emerald-400 mt-1">Configured (unchanged)</p>}
      {willClear && (
        <p className="text-xs text-amber-400 mt-1">
          Will be cleared on save{' '}
          <button
            type="button"
            onClick={() => onChange(REDACTED_SECRET)}
            className="underline text-gray-300 hover:text-white"
          >
            Undo
          </button>
        </p>
      )}
      {clearable && configured && value !== '' && (
        <button
          type="button"
          onClick={() => onChange('')}
          className="mt-1 text-xs text-red-400/80 hover:text-red-400 underline"
        >
          Clear
        </button>
      )}
    </div>
  )
}
