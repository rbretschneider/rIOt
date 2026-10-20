package handlers

func (h *Handlers) effectiveLogRetention() (days int, limitedBy string) {
	days = h.containerLogRetentionDays
	if days < 1 || days > 90 {
		days = 7
	}
	if h.storageGuard != nil {
		s := h.storageGuard.State()
		if s.RetentionLimitedBy != "" {
			return s.EffectiveRetentionDays, s.RetentionLimitedBy
		}
	}
	return days, ""
}
