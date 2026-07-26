import React, { useCallback, useEffect, useId, useRef, useState } from 'react';
import {
  X,
  Send,
  Paperclip,
  Image as ImageIcon,
  Bot,
  User,
  Sparkles,
  CheckCircle2,
  Loader2,
} from 'lucide-react';
import { useAuth } from '../../context/AuthContext';
import { apiFetch } from '../../utils/api';
import MarkdownContent from '../Common/MarkdownContent';
import {
  emptyDraft,
  welcomeMessages,
  respondToUser,
  respondToImage,
  isDraftReady,
  missingFields,
  draftSummaryLines,
  formatAltPrice,
  buildPlaceholderImage,
  toBase64,
  buildInscribePayload,
  applyDraftPatch,
  FUNDING_PAYOUT,
  FUNDING_RAISE,
} from './wishChatBot';

let msgSeq = 0;
const nextId = () => `m-${Date.now()}-${++msgSeq}`;

const WishChatModal = ({ onClose, onSuccess }) => {
  const { auth } = useAuth();
  const fileInputRef = useRef(null);
  const listRef = useRef(null);
  const inputRef = useRef(null);
  const titleId = useId();

  const [draft, setDraft] = useState(() => emptyDraft(auth.wallet || ''));
  const [messages, setMessages] = useState(() =>
    welcomeMessages(auth.wallet || '').map((m) => ({ ...m, id: m.id || nextId() })),
  );
  const [input, setInput] = useState('');
  const [phase, setPhase] = useState('collecting'); // collecting | ready | submitting | done | error
  const [isDragging, setIsDragging] = useState(false);
  const [errorText, setErrorText] = useState('');
  const [result, setResult] = useState(null);
  const dragDepth = useRef(0);

  // Keep wallet in sync with auth
  useEffect(() => {
    setDraft((d) => (d.address === (auth.wallet || '') ? d : { ...d, address: auth.wallet || '' }));
  }, [auth.wallet]);

  // Auto-scroll chat
  useEffect(() => {
    const el = listRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [messages, phase]);

  // Focus composer on open
  useEffect(() => {
    const t = setTimeout(() => inputRef.current?.focus(), 50);
    return () => clearTimeout(t);
  }, []);

  // Cleanup object URLs
  useEffect(() => {
    return () => {
      if (draft.imagePreviewUrl) {
        try {
          URL.revokeObjectURL(draft.imagePreviewUrl);
        } catch {
          /* ignore */
        }
      }
    };
    // only on unmount
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const pushMessages = useCallback((incoming) => {
    setMessages((prev) => [
      ...prev,
      ...incoming.map((m) => ({ ...m, id: m.id || nextId() })),
    ]);
  }, []);

  const submitInscription = useCallback(
    async (activeDraft) => {
      setPhase('submitting');
      setErrorText('');
      try {
        const uploadImage = activeDraft.imageFile || buildPlaceholderImage();
        const payload = buildInscribePayload(activeDraft);
        payload.image_base64 = await toBase64(uploadImage);
        payload.filename = uploadImage.name;

        const response = await apiFetch('/api/inscribe', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-API-Key': auth.apiKey || '',
          },
          body: JSON.stringify(payload),
        });

        if (!response.ok) {
          let detail = `Inscription failed (${response.status})`;
          try {
            const errBody = await response.json();
            if (errBody?.error || errBody?.message) {
              detail = errBody.error || errBody.message;
            }
          } catch {
            /* ignore */
          }
          throw new Error(detail);
        }

        const body = await response.json();
        setResult(body);
        setPhase('done');
        pushMessages([
          {
            role: 'bot',
            kind: 'success',
            content: [
              '✅ **Wish submitted for inscription.**',
              '',
              body?.id ? `Inscription ID: \`${body.id}\`` : '',
              'It is now pending — track it under Pending Transactions.',
            ]
              .filter(Boolean)
              .join('\n'),
          },
        ]);
        if (onSuccess) onSuccess();
      } catch (err) {
        const msg = err?.message || 'Something went wrong while inscribing.';
        setErrorText(msg);
        setPhase('error');
        pushMessages([
          {
            role: 'bot',
            kind: 'error',
            content: `❌ ${msg}\n\nYou can fix the draft and try again with **yes** / **inscribe**.`,
          },
        ]);
      }
    },
    [auth.apiKey, onSuccess, pushMessages],
  );

  const handleUserText = useCallback(
    async (text) => {
      const trimmed = String(text || '').trim();
      if (!trimmed || phase === 'submitting') return;

      pushMessages([{ role: 'user', content: trimmed }]);
      setInput('');

      const { messages: botMsgs, draft: nextDraft, phase: nextPhase } = respondToUser({
        userText: trimmed,
        draft,
      });
      setDraft(nextDraft);
      pushMessages(botMsgs);

      if (nextPhase === 'submitting') {
        await submitInscription(nextDraft);
      } else {
        setPhase(nextPhase);
      }
    },
    [draft, phase, pushMessages, submitInscription],
  );

  const handleFiles = useCallback(
    (fileList) => {
      if (phase === 'submitting' || phase === 'done') return;
      const file = Array.from(fileList || []).find((f) => f.type.startsWith('image/'));
      if (!file) {
        pushMessages([
          {
            role: 'bot',
            content: 'Please drop an **image** file (PNG, JPEG, WebP, GIF, …).',
          },
        ]);
        return;
      }
      pushMessages([
        {
          role: 'user',
          kind: 'image',
          content: `📎 ${file.name}`,
          imageFile: file,
          imagePreviewUrl: URL.createObjectURL(file),
        },
      ]);
      const { messages: botMsgs, draft: nextDraft, phase: nextPhase } = respondToImage(draft, file);
      setDraft(nextDraft);
      setPhase(nextPhase);
      pushMessages(botMsgs);
    },
    [draft, phase, pushMessages],
  );

  const onDragEnter = (e) => {
    e.preventDefault();
    e.stopPropagation();
    dragDepth.current += 1;
    if (e.dataTransfer?.types?.includes('Files')) setIsDragging(true);
  };
  const onDragLeave = (e) => {
    e.preventDefault();
    e.stopPropagation();
    dragDepth.current -= 1;
    if (dragDepth.current <= 0) {
      dragDepth.current = 0;
      setIsDragging(false);
    }
  };
  const onDragOver = (e) => {
    e.preventDefault();
    e.stopPropagation();
  };
  const onDrop = (e) => {
    e.preventDefault();
    e.stopPropagation();
    dragDepth.current = 0;
    setIsDragging(false);
    if (e.dataTransfer?.files?.length) {
      handleFiles(e.dataTransfer.files);
    }
  };

  const ready = isDraftReady(draft);
  const missing = missingFields(draft);

  const patchDraftField = (patch, botNote) => {
    if (phase === 'submitting' || phase === 'done') return;
    const next = applyDraftPatch(draft, patch);
    setDraft(next);
    setPhase(isDraftReady(next) ? 'ready' : 'collecting');
    if (botNote) {
      pushMessages([{ role: 'bot', content: botNote }]);
    }
  };

  const startAnother = () => {
    if (draft.imagePreviewUrl) {
      try {
        URL.revokeObjectURL(draft.imagePreviewUrl);
      } catch {
        /* ignore */
      }
    }
    const fresh = emptyDraft(auth.wallet || '');
    setDraft(fresh);
    setResult(null);
    setErrorText('');
    setPhase('collecting');
    setMessages(welcomeMessages(auth.wallet || '').map((m) => ({ ...m, id: nextId() })));
  };

  return (
    <div
      className="modal-backdrop-overlay wish-chat-backdrop"
      role="presentation"
      onClick={(e) => {
        if (e.target === e.currentTarget && phase !== 'submitting') onClose();
      }}
    >
      <div
        className="modal-container create-contract-modal wish-chat-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        onDragEnter={onDragEnter}
        onDragLeave={onDragLeave}
        onDragOver={onDragOver}
        onDrop={onDrop}
      >
        {isDragging && (
          <div className="wish-chat-drop-overlay" aria-hidden="true">
            <ImageIcon className="w-10 h-10 mb-2" />
            <div className="font-semibold">Drop image to attach</div>
          </div>
        )}

        <header className="wish-chat-header">
          <div className="flex items-center gap-2 min-w-0">
            <span className="wish-chat-avatar wish-chat-avatar-bot" aria-hidden="true">
              <Sparkles className="w-4 h-4" />
            </span>
            <div className="min-w-0">
              <h2 id={titleId} className="text-lg font-bold create-contract-title wish-chat-title">
                WishBot
              </h2>
              <p className="text-xs wish-chat-subtitle truncate">
                Nanobot · create a wish · drag images welcome
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="create-contract-close"
            aria-label="Close"
            disabled={phase === 'submitting'}
          >
            <X className="w-5 h-5" />
          </button>
        </header>

        <div className="wish-chat-body">
          <div className="wish-chat-messages" ref={listRef} role="log" aria-live="polite">
            {messages.map((m) => (
              <ChatBubble key={m.id} message={m} />
            ))}
            {phase === 'submitting' && (
              <div className="wish-chat-row wish-chat-row-bot">
                <span className="wish-chat-avatar wish-chat-avatar-bot">
                  <Bot className="w-3.5 h-3.5" />
                </span>
                <div className="wish-chat-bubble wish-chat-bubble-bot flex items-center gap-2">
                  <Loader2 className="w-4 h-4 animate-spin" />
                  Inscribing your wish…
                </div>
              </div>
            )}
          </div>

          <aside className="wish-chat-draft" aria-label="Wish draft">
            <div className="wish-chat-draft-title">Draft</div>
            <label className="wish-chat-field">
              <span>Wish</span>
              <textarea
                rows={2}
                value={draft.message}
                disabled={phase === 'submitting' || phase === 'done'}
                onChange={(e) => patchDraftField({ message: e.target.value })}
                placeholder="What should agents deliver?"
              />
            </label>
            <div className="wish-chat-field-row">
              <label className="wish-chat-field flex-1">
                <span>Price</span>
                <input
                  type="number"
                  min="0"
                  step={draft.priceUnit === 'sats' ? '1' : '0.00000001'}
                  value={draft.price}
                  disabled={phase === 'submitting' || phase === 'done'}
                  onChange={(e) => patchDraftField({ price: e.target.value })}
                  placeholder={draft.priceUnit === 'sats' ? '10000' : '0.0001'}
                />
              </label>
              <label className="wish-chat-field wish-chat-unit">
                <span>Unit</span>
                <select
                  value={draft.priceUnit}
                  disabled={phase === 'submitting' || phase === 'done'}
                  onChange={(e) => patchDraftField({ priceUnit: e.target.value })}
                >
                  <option value="sats">sats</option>
                  <option value="btc">BTC</option>
                </select>
              </label>
            </div>
            {draft.price !== '' && Number.isFinite(Number(draft.price)) && (
              <div className="wish-chat-hint">≈ {formatAltPrice(draft.price, draft.priceUnit)}</div>
            )}
            <label className="wish-chat-field">
              <span>Funding</span>
              <select
                value={draft.fundingMode}
                disabled={phase === 'submitting' || phase === 'done'}
                onChange={(e) => patchDraftField({ fundingMode: e.target.value })}
              >
                <option value={FUNDING_PAYOUT}>Payout to contractors</option>
                <option value={FUNDING_RAISE}>Raise fund from investors</option>
              </select>
            </label>
            <div className="wish-chat-field">
              <span>Image</span>
              <div className="wish-chat-image-row">
                {draft.imagePreviewUrl ? (
                  <img src={draft.imagePreviewUrl} alt="Wish cover" className="wish-chat-thumb" />
                ) : (
                  <div className="wish-chat-thumb wish-chat-thumb-empty" aria-hidden="true">
                    <ImageIcon className="w-4 h-4" />
                  </div>
                )}
                <div className="wish-chat-image-actions">
                  <button
                    type="button"
                    className="wish-chat-chip"
                    disabled={phase === 'submitting' || phase === 'done'}
                    onClick={() => fileInputRef.current?.click()}
                  >
                    {draft.imageFile ? 'Replace' : 'Add image'}
                  </button>
                  {draft.imageFile && phase !== 'done' && (
                    <button
                      type="button"
                      className="wish-chat-chip wish-chat-chip-muted"
                      onClick={() =>
                        patchDraftField({ imageFile: null, imagePreviewUrl: null }, 'Image removed.')
                      }
                    >
                      Remove
                    </button>
                  )}
                  <span className="wish-chat-hint truncate">
                    {draft.imageFile ? draft.imageFile.name : 'Optional'}
                  </span>
                </div>
              </div>
            </div>
            <div className="wish-chat-field">
              <span>Wallet</span>
              <div className={`wish-chat-wallet ${draft.address ? '' : 'wish-chat-wallet-missing'}`}>
                {draft.address || 'Not signed in'}
              </div>
            </div>
            <div className="wish-chat-draft-status">
              {phase === 'done' ? (
                <span className="wish-chat-ready">
                  <CheckCircle2 className="w-3.5 h-3.5" /> Submitted
                  {result?.id ? ` · ${result.id}` : ''}
                </span>
              ) : ready ? (
                <span className="wish-chat-ready">Ready to inscribe</span>
              ) : (
                <span className="wish-chat-missing">Need: {missing.join(', ') || '—'}</span>
              )}
            </div>
            {phase !== 'done' && (
              <button
                type="button"
                className="wish-chat-inscribe-btn"
                disabled={!ready || phase === 'submitting'}
                onClick={() => {
                  pushMessages([{ role: 'user', content: 'inscribe' }]);
                  pushMessages([
                    {
                      role: 'bot',
                      content: [
                        'Submitting with this draft:',
                        '',
                        ...draftSummaryLines(draft),
                      ].join('\n'),
                      kind: 'summary',
                    },
                  ]);
                  submitInscription(draft);
                }}
              >
                {phase === 'submitting' ? 'Inscribing…' : 'Inscribe wish'}
              </button>
            )}
            {phase === 'done' && (
              <div className="wish-chat-done-actions">
                <button type="button" className="wish-chat-chip" onClick={startAnother}>
                  Create another
                </button>
                <button type="button" className="wish-chat-inscribe-btn" onClick={onClose}>
                  Done
                </button>
              </div>
            )}
            {errorText && phase === 'error' && (
              <div className="wish-chat-error" role="alert">
                {errorText}
              </div>
            )}
          </aside>
        </div>

        {phase !== 'done' && (
          <footer className="wish-chat-composer">
            <input
              ref={fileInputRef}
              type="file"
              accept="image/*"
              className="hidden"
              onChange={(e) => {
                if (e.target.files?.length) handleFiles(e.target.files);
                e.target.value = '';
              }}
            />
            <button
              type="button"
              className="wish-chat-icon-btn"
              title="Attach image"
              aria-label="Attach image"
              disabled={phase === 'submitting'}
              onClick={() => fileInputRef.current?.click()}
            >
              <Paperclip className="w-4 h-4" />
            </button>
            <textarea
              ref={inputRef}
              className="wish-chat-input"
              rows={1}
              value={input}
              disabled={phase === 'submitting'}
              placeholder="Describe your wish, set a price…"
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault();
                  handleUserText(input);
                }
              }}
            />
            <button
              type="button"
              className="wish-chat-send-btn"
              aria-label="Send"
              disabled={!input.trim() || phase === 'submitting'}
              onClick={() => handleUserText(input)}
            >
              <Send className="w-4 h-4" />
            </button>
          </footer>
        )}
      </div>
    </div>
  );
};

function ChatBubble({ message }) {
  const isUser = message.role === 'user';
  return (
    <div className={`wish-chat-row ${isUser ? 'wish-chat-row-user' : 'wish-chat-row-bot'}`}>
      {!isUser && (
        <span className="wish-chat-avatar wish-chat-avatar-bot" aria-hidden="true">
          <Bot className="w-3.5 h-3.5" />
        </span>
      )}
      <div
        className={`wish-chat-bubble ${isUser ? 'wish-chat-bubble-user' : 'wish-chat-bubble-bot'} ${
          message.kind === 'error' ? 'wish-chat-bubble-error' : ''
        } ${message.kind === 'success' ? 'wish-chat-bubble-success' : ''}`}
      >
        {message.kind === 'image' && message.imagePreviewUrl && (
          <img src={message.imagePreviewUrl} alt="" className="wish-chat-msg-image" />
        )}
        {isUser ? (
          <div className="whitespace-pre-wrap break-words">{message.content}</div>
        ) : (
          <MarkdownContent className="wish-chat-md" compact>
            {message.content}
          </MarkdownContent>
        )}
      </div>
      {isUser && (
        <span className="wish-chat-avatar wish-chat-avatar-user" aria-hidden="true">
          <User className="w-3.5 h-3.5" />
        </span>
      )}
    </div>
  );
}

export default WishChatModal;
