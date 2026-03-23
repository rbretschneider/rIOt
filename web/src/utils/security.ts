/**
 * Shared security score colour and grade utilities.
 * Grade thresholds match the server-side scoring engine exactly:
 *   >= 90  -> A
 *   >= 75  -> B
 *   >= 60  -> C
 *   >= 40  -> D
 *   < 40   -> F
 */

/** Returns the Tailwind text-colour class for a letter grade. */
export function gradeColor(grade: string): string {
  switch (grade) {
    case 'A': return 'text-emerald-400'
    case 'B': return 'text-blue-400'
    case 'C': return 'text-amber-400'
    case 'D': return 'text-orange-400'
    default:  return 'text-red-400'
  }
}

/** Returns the Tailwind stroke-colour class for a letter grade (radial indicator). */
export function gradeStrokeColor(grade: string): string {
  switch (grade) {
    case 'A': return 'stroke-emerald-500'
    case 'B': return 'stroke-blue-500'
    case 'C': return 'stroke-amber-500'
    case 'D': return 'stroke-orange-500'
    default:  return 'stroke-red-500'
  }
}

/** Derives the letter grade from a numeric score (0–100). */
export function gradeFromScore(score: number): string {
  if (score >= 90) return 'A'
  if (score >= 75) return 'B'
  if (score >= 60) return 'C'
  if (score >= 40) return 'D'
  return 'F'
}
