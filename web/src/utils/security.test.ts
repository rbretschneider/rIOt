import { describe, it, expect } from 'vitest'
import { gradeColor, gradeStrokeColor, gradeFromScore } from './security'

describe('gradeFromScore', () => {
  describe('[AC-002] Score column grade thresholds match server-side engine', () => {
    it('returns A for score >= 90', () => {
      expect(gradeFromScore(90)).toBe('A')
      expect(gradeFromScore(100)).toBe('A')
      expect(gradeFromScore(95)).toBe('A')
    })

    it('returns B for score >= 75 and < 90', () => {
      expect(gradeFromScore(75)).toBe('B')
      expect(gradeFromScore(89)).toBe('B')
    })

    it('returns C for score >= 60 and < 75', () => {
      expect(gradeFromScore(60)).toBe('C')
      expect(gradeFromScore(74)).toBe('C')
    })

    it('returns D for score >= 40 and < 60', () => {
      expect(gradeFromScore(40)).toBe('D')
      expect(gradeFromScore(59)).toBe('D')
    })

    it('returns F for score < 40', () => {
      expect(gradeFromScore(0)).toBe('F')
      expect(gradeFromScore(39)).toBe('F')
    })

    it('returns F for score exactly 39 (boundary check)', () => {
      expect(gradeFromScore(39)).toBe('F')
    })

    it('returns D for score exactly 40 (boundary check)', () => {
      expect(gradeFromScore(40)).toBe('D')
    })
  })
})

describe('gradeColor', () => {
  describe('[AC-002] Score column colour coding matches grade', () => {
    it('returns emerald for grade A', () => {
      expect(gradeColor('A')).toBe('text-emerald-400')
    })

    it('returns blue for grade B', () => {
      expect(gradeColor('B')).toBe('text-blue-400')
    })

    it('returns amber for grade C', () => {
      expect(gradeColor('C')).toBe('text-amber-400')
    })

    it('returns orange for grade D', () => {
      expect(gradeColor('D')).toBe('text-orange-400')
    })

    it('returns red for grade F', () => {
      expect(gradeColor('F')).toBe('text-red-400')
    })

    it('returns red for unknown grade (fallback)', () => {
      expect(gradeColor('X')).toBe('text-red-400')
    })
  })
})

describe('gradeStrokeColor', () => {
  describe('[AC-002] Radial indicator stroke colour matches grade', () => {
    it('returns emerald stroke for grade A', () => {
      expect(gradeStrokeColor('A')).toBe('stroke-emerald-500')
    })

    it('returns blue stroke for grade B', () => {
      expect(gradeStrokeColor('B')).toBe('stroke-blue-500')
    })

    it('returns amber stroke for grade C', () => {
      expect(gradeStrokeColor('C')).toBe('stroke-amber-500')
    })

    it('returns orange stroke for grade D', () => {
      expect(gradeStrokeColor('D')).toBe('stroke-orange-500')
    })

    it('returns red stroke for grade F', () => {
      expect(gradeStrokeColor('F')).toBe('stroke-red-500')
    })
  })
})
