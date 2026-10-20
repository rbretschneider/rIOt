import { Popover } from '../ui/Popover'
export default function MultiSelectFilter({
  label,
  options,
  selected,
  onChange,
}: {
  label: string
  options: { value: string; label: string }[]
  selected: string[]
  onChange: (values: string[]) => void
}) {
  return (
    <Popover
      label={`${label}: ${selected.length ? `${selected.length} selected` : 'All'}`}
    >
      <fieldset>
        <legend className="sr-only">{label} filter</legend>
        <button
          type="button"
          className="text-link text-xs mb-2"
          onClick={() => onChange([])}
        >
          Clear selection
        </button>
        {options.map((o) => (
          <label key={o.value} className="flex items-center gap-2 py-1 text-sm">
            <input
              type="checkbox"
              checked={selected.includes(o.value)}
              onChange={() =>
                onChange(
                  selected.includes(o.value)
                    ? selected.filter((v) => v !== o.value)
                    : [...selected, o.value],
                )
              }
            />
            {o.label}
          </label>
        ))}
      </fieldset>
    </Popover>
  )
}
