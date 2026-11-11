'use client';
import { useEffect, useState, use } from 'react';
import { useRouter } from 'next/navigation';
import {
  Download,
  CheckCircle2,
  Clock,
  FileText,
  Copy,
  Check,
  ExternalLink,
  ShieldCheck,
  RefreshCw,
  Users,
  Calendar,
} from 'lucide-react';
import Nav from '@/components/Nav';
import WorkflowStepper from '@/components/WorkflowStepper';
import { api, API_URL, getError } from '@/lib/api';
import { signingLink } from '@/lib/signing-link';

interface Signer {
  id: string;
  name: string;
  email: string;
  token: string;
  tokenExpiresAt: string;
  status: 'pending' | 'signed';
  signedAt?: string;
  createdAt: string;
}

interface AuditLog {
  id: string;
  action: string;
  performedBy: string;
  details: string;
  ipAddress?: string;
  createdAt: string;
}

interface StatusSummary {
  document: {
    id: string;
    title: string;
    originalName: string;
    status: string;
    fileSize: number;
    createdAt: string;
    signedFileName?: string;
  };
  signers: Signer[];
  auditLogs: AuditLog[];
  totalSigners: number;
  signedCount: number;
  progress: number;
  hasSignedPdf: boolean;
}

export default function DocumentStatusPage({ params }: { params: Promise<{ id: string }> }) {
  const router = useRouter();
  const resolvedParams = use(params);
  const docId = resolvedParams.id;

  const [summary, setSummary] = useState<StatusSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [downloading, setDownloading] = useState(false);
  const [copiedToken, setCopiedToken] = useState<string | null>(null);
  const [linkError, setLinkError] = useState('');

  useEffect(() => {
    const token = localStorage.getItem('token');
    if (!token) {
      router.replace('/login');
      return;
    }
    loadStatus();
  }, [docId, router]);

  async function loadStatus() {
    try {
      setLoading(true);
      setError('');
      const res = await api.get(`/api/documents/${docId}/status`);
      setSummary(res.data);
    } catch (err) {
      setError(getError(err));
    } finally {
      setLoading(false);
    }
  }

  async function downloadSignedPdf() {
    try {
      setDownloading(true);
      const res = await api.get(`/api/documents/${docId}/signed-download`, {
        responseType: 'blob',
      });
      const url = URL.createObjectURL(res.data);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${summary?.document.title || 'document'}_signed.pdf`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      setTimeout(() => URL.revokeObjectURL(url), 60000);
    } catch (err) {
      setError(getError(err));
    } finally {
      setDownloading(false);
    }
  }

  async function getCurrentSigner(signerId: string): Promise<{ signer: Signer | null; documentStatus: string }> {
    const response = await api.get(`/api/documents/${docId}/status`);
    const latest: StatusSummary = response.data;
    setSummary(latest);
    return {
      signer: latest.signers.find((item) => item.id === signerId) || null,
      documentStatus: latest.document.status,
    };
  }

  function isTokenExpired(signer: Signer): boolean {
    const expiresAt = Date.parse(signer.tokenExpiresAt);
    return !Number.isFinite(expiresAt) || expiresAt <= Date.now();
  }

  async function copySignerLink(signerId: string) {
    setLinkError('');
    try {
      const { signer, documentStatus } = await getCurrentSigner(signerId);
      if (!signer || signer.status !== 'pending' || !signer.token) {
        setLinkError('This signer is no longer pending. Refresh the status to see the latest state.');
        return;
      }
      if (documentStatus !== 'pending') {
        setLinkError(documentStatus === 'draft'
          ? 'This document is still a draft. Send it for signing before sharing a link.'
          : 'This document is not accepting signatures.');
        return;
      }
      if (isTokenExpired(signer)) {
        setLinkError('This signing link has expired. Resend the signing request to issue a current link.');
        return;
      }
      await navigator.clipboard.writeText(signingLink(signer.token));
      setCopiedToken(signer.id);
      setTimeout(() => setCopiedToken(null), 2500);
    } catch (err) {
      setLinkError(getError(err));
    }
  }

  async function openSignerLink(signerId: string) {
    setLinkError('');
    const popup = window.open('about:blank', '_blank');
    if (!popup) {
      setLinkError('Allow pop-ups to open the signing page.');
      return;
    }
    popup.opener = null;
    try {
      const { signer, documentStatus } = await getCurrentSigner(signerId);
      if (!signer || signer.status !== 'pending' || !signer.token) {
        popup.close();
        setLinkError('This signer is no longer pending. Refresh the status to see the latest state.');
        return;
      }
      if (documentStatus !== 'pending') {
        popup.close();
        setLinkError(documentStatus === 'draft'
          ? 'This document is still a draft. Send it for signing before opening a link.'
          : 'This document is not accepting signatures.');
        return;
      }
      if (isTokenExpired(signer)) {
        popup.close();
        setLinkError('This signing link has expired. Resend the signing request to issue a current link.');
        return;
      }
      popup.location.href = signingLink(signer.token);
    } catch (err) {
      popup.close();
      setLinkError(getError(err));
    }
  }

  return (
    <>
      <Nav />
      <main className="shell">
        <div className="row" style={{ marginBottom: 14 }}>
          <div>
            <h1 className="title">Document Status & Activity</h1>
            <p className="muted" style={{ margin: 0 }}>
              {summary ? `Title: ${summary.document.title}` : 'Real-time document signing progress.'}
            </p>
          </div>
          <div className="actions">
            <button className="btn secondary sm" onClick={loadStatus} disabled={loading}>
              <RefreshCw size={14} /> Refresh
            </button>

            {/* Download button appears ONLY when the document is completed */}
            {summary?.document.status === 'completed' ? (
              <button
                className="btn success"
                onClick={downloadSignedPdf}
                disabled={downloading}
              >
                <Download size={16} />{' '}
                {downloading ? 'Downloading...' : 'Download Completed Signed PDF'}
              </button>
            ) : (
              <div
                style={{
                  fontSize: 12.5,
                  color: '#64748b',
                  display: 'flex',
                  alignItems: 'center',
                  gap: 6,
                  padding: '8px 12px',
                  background: '#f1f5f9',
                  borderRadius: 8,
                }}
              >
                <Clock size={15} /> Signed PDF unlocks when all signers complete
              </div>
            )}
          </div>
        </div>

        <WorkflowStepper docId={docId} currentStep="status" />

        {error && <div className="error" style={{ marginBottom: 16 }}>{error}</div>}
        {linkError && <div className="error" role="alert" style={{ marginBottom: 16 }}>{linkError}</div>}

        {loading ? (
          <p className="muted">Loading status details...</p>
        ) : summary ? (
          <>
            {/* Progress Card */}
            <div className="card" style={{ marginBottom: 20 }}>
              <div className="row" style={{ marginBottom: 14 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                  <Users size={22} color="#4f46e5" />
                  <h2 style={{ margin: 0, fontSize: 18, fontWeight: 700 }}>Signing Progress</h2>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                  <span className={`badge ${summary.document.status}`}>
                    {summary.document.status}
                  </span>
                  <span style={{ fontSize: 16, fontWeight: 800, color: '#4f46e5' }}>
                    {summary.progress}%
                  </span>
                </div>
              </div>

              {/* Progress Bar */}
              <div className="progress-bar-bg" style={{ marginBottom: 10 }}>
                <div
                  className="progress-bar-fill"
                  style={{ width: `${summary.progress}%` }}
                />
              </div>

              <div className="row" style={{ fontSize: 13, color: '#64748b' }}>
                <span>
                  <strong>{summary.signedCount}</strong> of <strong>{summary.totalSigners}</strong>{' '}
                  signers completed signatures
                </span>
                <span>
                  {summary.totalSigners - summary.signedCount} pending recipient(s)
                </span>
              </div>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(340px, 1fr))', gap: 20 }}>
              {/* Signers Status List */}
              <div className="card">
                <h2 style={{ fontSize: 18, fontWeight: 700, margin: '0 0 16px' }}>
                  Signers List ({summary.signers.length})
                </h2>

                <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                  {summary.signers.map((signer) => {
                    const isSigned = signer.status === 'signed';
                    return (
                      <div
                        key={signer.id}
                        style={{
                          padding: '14px',
                          borderRadius: 10,
                          border: `1px solid ${isSigned ? '#bbf7d0' : '#e2e8f0'}`,
                          background: isSigned ? '#f0fdf4' : '#fff',
                        }}
                      >
                        <div className="row" style={{ marginBottom: 6 }}>
                          <div>
                            <span style={{ fontWeight: 700, fontSize: 14.5 }}>
                              {signer.name}
                            </span>
                            <div className="muted" style={{ fontSize: 12.5 }}>
                              {signer.email}
                            </div>
                          </div>
                          <span className={`badge ${signer.status}`}>
                            {isSigned ? (
                              <CheckCircle2 size={12} style={{ marginRight: 3 }} />
                            ) : (
                              <Clock size={12} style={{ marginRight: 3 }} />
                            )}
                            {signer.status}
                          </span>
                        </div>

                        {isSigned ? (
                          <div
                            style={{
                              fontSize: 12,
                              color: '#15803d',
                              display: 'flex',
                              alignItems: 'center',
                              gap: 6,
                              marginTop: 6,
                            }}
                          >
                            <CheckCircle2 size={14} /> Signed on{' '}
                            {signer.signedAt ? new Date(signer.signedAt).toLocaleString() : 'N/A'}
                          </div>
                        ) : summary.document.status === 'draft' ? (
                          <div className="muted" style={{ marginTop: 8, fontSize: 12.5 }}>
                            Draft — send the document before sharing a signing link.
                          </div>
                        ) : summary.document.status !== 'pending' ? (
                          <div className="muted" style={{ marginTop: 8, fontSize: 12.5 }}>
                            Document is not accepting signatures.
                          </div>
                        ) : isTokenExpired(signer) ? (
                          <div style={{ marginTop: 8, fontSize: 12.5, color: '#b45309' }}>
                            Signing link expired — resend the request to renew it.
                          </div>
                        ) : (
                          <div style={{ marginTop: 8, display: 'flex', gap: 8, alignItems: 'center' }}>
                            <button
                              className="btn sm secondary"
                              onClick={() => copySignerLink(signer.id)}
                            >
                              {copiedToken === signer.id ? (
                                <Check size={13} color="#16a34a" />
                              ) : (
                                <Copy size={13} />
                              )}
                              {copiedToken === signer.id ? 'Link Copied' : 'Copy Signing Link'}
                            </button>
                            <button
                              type="button"
                              onClick={() => openSignerLink(signer.id)}
                              className="btn sm ghost"
                              style={{ fontSize: 12 }}
                            >
                              Open <ExternalLink size={12} />
                            </button>
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              </div>

              {/* Audit Trail Timeline */}
              <div className="card">
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 16 }}>
                  <ShieldCheck size={20} color="#4f46e5" />
                  <h2 style={{ fontSize: 18, fontWeight: 700, margin: 0 }}>
                    Audit Trail & History
                  </h2>
                </div>

                {summary.auditLogs.length === 0 ? (
                  <p className="muted">No activity logged yet.</p>
                ) : (
                  <div className="timeline">
                    {summary.auditLogs.map((log) => {
                      const isCompleted = log.action === 'DOCUMENT_COMPLETED';
                      const isSigned = log.action === 'DOCUMENT_SIGNED';

                      return (
                        <div key={log.id} className="timeline-item">
                          <div
                            className={`timeline-dot ${isCompleted || isSigned ? 'done' : ''}`}
                          />
                          <div className="timeline-content">
                            <div className="row" style={{ marginBottom: 3 }}>
                              <strong style={{ color: '#1e293b' }}>
                                {log.action.replace(/_/g, ' ')}
                              </strong>
                              <span className="muted" style={{ fontSize: 11 }}>
                                {new Date(log.createdAt).toLocaleTimeString([], {
                                  hour: '2-digit',
                                  minute: '2-digit',
                                  second: '2-digit',
                                })}{' '}
                                · {new Date(log.createdAt).toLocaleDateString()}
                              </span>
                            </div>
                            <div style={{ fontSize: 13, color: '#334155' }}>
                              {log.details}
                            </div>
                            <div className="muted" style={{ fontSize: 11.5, marginTop: 2 }}>
                              By: {log.performedBy} {log.ipAddress ? `(${log.ipAddress})` : ''}
                            </div>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            </div>
          </>
        ) : null}
      </main>
    </>
  );
}
