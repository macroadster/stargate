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

/** Normalize submissions props (array | object | list) into map + array. */
export const normalizeSubmissions = (submissions, submissionsList) => {
  const submissionsObj =
    submissions && !Array.isArray(submissions) && typeof submissions === 'object' ? submissions : {};
  let submissionsArray = [];
  try {
    if (Array.isArray(submissionsList)) {
      submissionsArray = submissionsList;
    } else if (Array.isArray(submissions)) {
      submissionsArray = submissions;
    } else if (submissionsObj) {
      submissionsArray = Object.keys(submissionsObj).map((key) => submissionsObj[key]);
    }
  } catch (e) {
    console.error('Error processing submissions:', e, submissions);
    submissionsArray = [];
  }
  const submissionsMap = {};
  submissionsArray.forEach((submission) => {
    if (submission.submission_id) submissionsMap[submission.submission_id] = submission;
    if (submission.task_id) {
      const existing = submissionsMap[submission.task_id];
      if (!existing || getSubmissionTimestamp(submission) > getSubmissionTimestamp(existing)) {
        submissionsMap[submission.task_id] = submission;
      }
    }
    if (submission.claim_id) {
      const existing = submissionsMap[submission.claim_id];
      if (!existing || getSubmissionTimestamp(submission) > getSubmissionTimestamp(existing)) {
        submissionsMap[submission.claim_id] = submission;
      }
    }
  });
  return { submissionsObj, submissionsArray, submissionsMap };
};

/** Join proposal tasks with matching submissions. */
export const buildAllDeliverables = (proposalItems, submissionsObj) => {
  // Determine the latest attempt per task by timestamp instead of relying
  // on a plain map lookup, which can point at the first (oldest) attempt
  // when the map was built by simple overwrite.
  const candidates = [];
  if (Array.isArray(submissionsObj)) {
    candidates.push(...submissionsObj);
  } else if (submissionsObj && typeof submissionsObj === 'object') {
    Object.values(submissionsObj).forEach((s) => {
      if (s && typeof s === 'object') candidates.push(s);
    });
  }
  const unique = new Map();
  candidates.forEach((s) => {
    const key = s.submission_id || `${s.task_id || ''}:${getSubmissionTimestamp(s)}:${s.claim_id || ''}`;
    if (!unique.has(key)) unique.set(key, s);
  });
  const uniq = Array.from(unique.values());
  const latestByTaskId = {};
  const latestByClaimId = {};
  uniq.forEach((s) => {
    if (s.task_id) {
      const existing = latestByTaskId[s.task_id];
      if (!existing || getSubmissionTimestamp(s) > getSubmissionTimestamp(existing)) {
        latestByTaskId[s.task_id] = s;
      }
    }
    if (s.claim_id) {
      const existing = latestByClaimId[s.claim_id];
      if (!existing || getSubmissionTimestamp(s) > getSubmissionTimestamp(existing)) {
        latestByClaimId[s.claim_id] = s;
      }
    }
  });
  return (proposalItems || [])
    .flatMap((proposal) => {
      const tasks =
        Array.isArray(proposal.tasks) && proposal.tasks.length > 0
          ? proposal.tasks
          : Array.isArray(proposal.metadata?.suggested_tasks)
            ? proposal.metadata.suggested_tasks
            : [];
      return tasks.map((task) => {
        let submission =
          latestByTaskId[task.task_id]
          || (task.active_claim_id ? latestByClaimId[task.active_claim_id] : null)
          || null;
        // Fallback to direct map lookup for submissions that lack task/claim ids
        // or where the direct entry is newer than the scanned one.
        if (submissionsObj && !Array.isArray(submissionsObj)) {
          const direct = submissionsObj[task.task_id] || (task.active_claim_id ? submissionsObj[task.active_claim_id] : null) || null;
          if (direct && (!submission || getSubmissionTimestamp(direct) > getSubmissionTimestamp(submission))) {
            submission = direct;
          }
        }
        return {
          ...task,
          proposal,
          submission,
          submissionKey: submission?.submission_id,
          proposalId: proposal.id,
          proposalTitle: proposal.title,
        };
      });
    })
    .filter((item) => item.submission);
};

export const buildComparisonGroups = (proposalItems, submissionsByTask) =>
  (proposalItems || [])
    .flatMap((proposal) => {
      const tasks =
        Array.isArray(proposal.tasks) && proposal.tasks.length > 0
          ? proposal.tasks
          : Array.isArray(proposal.metadata?.suggested_tasks)
            ? proposal.metadata.suggested_tasks
            : [];
      return tasks.map((task) => ({
        ...task,
        proposal,
        proposalId: proposal.id,
        proposalTitle: proposal.title,
        submissions: submissionsByTask[task.task_id] || [],
      }));
    })
    .filter((group) => group.submissions.length > 0);

export const groupSubmissionsByTask = (submissionsArray) =>
  submissionsArray.reduce((acc, submission) => {
    const taskId = submission?.task_id;
    if (!taskId) return acc;
    if (!acc[taskId]) acc[taskId] = [];
    acc[taskId].push(submission);
    return acc;
  }, {});
