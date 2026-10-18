// Optional contract for a React + TypeScript client.
export type SpamVerdict = 'spam' | 'not_spam' | 'uncertain';
export type Importance = 'low' | 'medium' | 'high' | 'uncertain';
export type MailCategory = 'personal' | 'work' | 'finance' | 'security' | 'shopping' | 'delivery' | 'account' | 'social' | 'newsletter' | 'promotion' | 'notification' | 'other' | 'uncertain';
export type ActionType = 'none' | 'reply' | 'review' | 'payment' | 'deadline' | 'security' | 'other';
export type JobStatus = 'pending' | 'processing' | 'completed' | 'retry' | 'skipped';
export type MailDeletionState = 'active' | 'moving' | 'deleted' | 'unknown';
export interface Classification {
  email_id: number;
  sender: string;
  subject: string;
  received_at: string;
  is_spam: boolean; // derived from spam_verdict
  spam_verdict: SpamVerdict;
  importance: Importance;
  category: MailCategory;
  action_required: boolean;
  action_type: ActionType;
  summary: string;
  reason: string;
  include_in_report: boolean;
  needs_review: boolean;
  body_truncated: boolean;
  original_chars: number;
  processed_chars: number;
  manual_override: boolean;
  reviewed_at: string;
  deletion_state: MailDeletionState;
  model: string;
}
export interface Page<T> { items: T[]; total: number; count: number; limit: number; offset: number }
export interface QueueStats { pending:number; processing:number; completed:number; retry:number; skipped:number; total:number }
export interface DailyReport {date:string;total_received:number;classified:number;spam:number;unprocessed:number;needs_review:number;items:Classification[]}
export type MoveStatus = 'moved_to_trash' | 'already_deleted' | 'not_found' | 'invalid_id' | 'conflict' | 'needs_reconciliation' | 'moved_on_server_db_error' | 'imap_move_unsupported' | 'uidvalidity_changed' | 'uid_not_found';
export interface MoveResult { id:number; status:MoveStatus }
export interface MoveResponse { items: MoveResult[]; count:number }
export interface ManualClassification {
  spam_verdict:SpamVerdict;
  importance:Importance;
  category:MailCategory;
  action_required:boolean;
  action_type:ActionType;
  summary:string;
  reason:string;
  include_in_report:boolean;
}

// Backend has exactly one administrator. No roles or user-management endpoints.
export interface AdminUser { id: number; username: string; active: boolean; created_at: string }
export interface AuthStatus { status: 'ok' }
export interface MailSettings {
 configured: boolean;
 host?: string; port?: number; username?: string; auth_method?: 'oauth2'|'app_password';
 folder?: string; trash_folder?: string; allow_delete?: boolean;
 poll_interval_seconds?: number; fetch_limit?: number; max_message_bytes?: number;
}
export interface SetMailSettings {
 host: 'imap.yandex.ru'|'imap.yandex.com';port: number;username:string;
 auth_method:'oauth2'|'app_password';oauth_token?:string;app_password?:string;
 folder:string;trash_folder:string;allow_delete:boolean;
 poll_interval_seconds:number;fetch_limit:number;max_message_bytes:number;
}
