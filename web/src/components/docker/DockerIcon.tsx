export default function DockerIcon({ kind }: { kind: string }) {
  const paths: Record<string, string> = {
    containers: 'M12 3 3 8v8l9 5 9-5V8l-9-5Zm0 9v9M3 8l9 5 9-5M7.5 5.5l9 5',
    stacks: 'm12 3 10 5-10 5L2 8l10-5ZM2 12l10 5 10-5M2 16l10 5 10-5',
    hosts: 'M3 3h18v7H3zM3 14h18v7H3zM6 6h.01M6 17h.01M10 6h8M10 17h8',
    images: 'M4 4h16v16H4zM4 15l5-5 5 5 3-3 3 3M16 8h.01',
    volumes:
      'M4 6c0-4 16-4 16 0s-16 4-16 0Zm0 0v12c0 4 16 4 16 0V6M4 12c0 4 16 4 16 0',
    networks: 'M9 3h6v6H9zM2 15h6v6H2zM16 15h6v6h-6zM12 9v3M5 15v-3h14v3',
    activity: 'M4 4v16h16M7 14l4-5 4 3 5-8',
  }
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      className="h-4 w-4 shrink-0"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d={paths[kind] ?? paths.containers} />
    </svg>
  )
}
