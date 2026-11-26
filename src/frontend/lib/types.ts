export interface User {
  id: number;
  username: string;
  is_admin: boolean;
  is_active: boolean;
}

export interface LeaderGroup {
  id: number;
  name: string;
}

export interface AuthResponse {
  user: User;
  leader_groups: LeaderGroup[];
  csrf_token: string;
}

export interface AdminUser extends User {
  groups: { id: number; name: string }[];
}

export interface GroupGrant {
  database_id: number;
  database_name: string;
  access_level: number; // 1 = view, 2 = view + edit
}

export interface AdminGroup {
  id: number;
  name: string;
  leader_id: number | null;
  leader_username: string;
  members: User[];
  grants: GroupGrant[];
}

export interface TargetDatabase {
  id: number;
  name: string;
  host: string;
  port: number;
  database_name: string;
  username: string;
  ssl_mode: string;
}

export interface RequiredApproval {
  group_id: number;
  group_name: string;
  access_level: number;
  leader_id: number | null;
  leader_username?: string;
  leader_active: boolean;
  approved: boolean;
  approved_at?: string;
}

export interface ApprovalStatus {
  job_id: number;
  database_id: number;
  action: string;
  status: string;
  all_approved: boolean;
  required: RequiredApproval[];
  can_approve_groups: number[];
}

export interface InboxItem {
  job_id: number;
  database_id: number;
  database_name: string;
  action: string;
  migration_version: number;
  migration_title: string;
  requested_by: string;
  created_at: string;
  waiting_groups: LeaderGroup[];
}
