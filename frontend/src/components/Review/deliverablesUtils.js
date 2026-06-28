// Pure helpers for DeliverablesReview

export const filterByKeys = (entries, allowed) => Object.keys(entries).reduce((acc, key) => {
  if (allowed.has(key)) {
    acc[key] = entries[key];
  }
  return acc;
}, {});

export const countWords = (value) => {
  if (!value || typeof value !== 'string') return 0;
  return value.trim().split(/\s+/).filter(Boolean).length;
};

export const getSubmissionNotes = (submission) => (
  submission?.deliverables?.notes
    || submission?.deliverables?.document
    || submission?.deliverables?.rework_notes
    || ''
);

export const getNotesPreview = (value, limit = 320) => {
  if (!value) return '';
  if (value.length <= limit) return value;
  return `${value.slice(0, limit)}…`;
};

export const getSubmissionTimestamp = (submission) => {
  const raw = submission?.submitted_at || submission?.created_at;
  if (!raw) return 0;
  const parsed = Date.parse(raw);
  return Number.isNaN(parsed) ? 0 : parsed;
};

export const getSubmissionId = (submission, fallback) => (
  submission?.submission_id || submission?.id || fallback
);

export const formatTimestamp = (value) => (
  value ? new Date(value).toLocaleString() : 'Unknown time'
);

export const sortSubmissions = (submissions, sortBy = 'newest') => {
  return [...submissions].sort((a, b) => {
    if (sortBy === 'most_words' || sortBy === 'fewest_words') {
      const aWords = countWords(getSubmissionNotes(a));
      const bWords = countWords(getSubmissionNotes(b));
      return sortBy === 'most_words' ? bWords - aWords : aWords - bWords;
    }
    const aTime = getSubmissionTimestamp(a);
    const bTime = getSubmissionTimestamp(b);
    return sortBy === 'oldest' ? aTime - bTime : bTime - aTime;
  });
};
