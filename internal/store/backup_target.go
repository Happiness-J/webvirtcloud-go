package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (db *Store) SaveBackupTarget(ctx context.Context, policy BackupPolicy) error {
	if policy.TargetKind == "" {
		policy.TargetKind = "local"
	}
	if err := validBackupTarget(policy.TargetKind, policy.TargetRef); err != nil {
		return err
	}
	tag, err := db.pool.Exec(ctx, `UPDATE backup_policies SET target_kind=$3, target_ref=$4 WHERE host_id=$1 AND vm_name=$2`,
		policy.HostID, policy.VMName, policy.TargetKind, policy.TargetRef)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("请先保存定时备份策略")
	}
	return nil
}

func validBackupTarget(kind, ref string) error {
	switch kind {
	case "local":
		if ref != "" {
			return fmt.Errorf("本地目标不需要地址")
		}
	case "nfs":
		host, path, ok := strings.Cut(ref, ":")
		if !ok || host == "" || !strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
			return fmt.Errorf("NFS 目标必须是主机:/导出路径")
		}
	case "s3":
		if !strings.HasPrefix(ref, "s3://") || strings.Contains(ref, "..") || strings.ContainsAny(ref, " \t\r\n") {
			return fmt.Errorf("对象存储目标必须是 s3:// 地址")
		}
	default:
		return fmt.Errorf("备份目标类型无效")
	}
	if len(ref) > 300 {
		return fmt.Errorf("备份目标过长")
	}
	return nil
}

type BackupDrill struct {
	ID         string
	HostID     string
	VMName     string
	TargetKind string
	TargetRef  string
	Checksum   string
	Result     string
	Note       string
	ActorName  string
	CreatedAt  time.Time
}

func (db *Store) AddBackupDrill(ctx context.Context, drill BackupDrill) error {
	if drill.Result != "passed" && drill.Result != "failed" {
		return fmt.Errorf("演练结果无效")
	}
	if len(drill.Checksum) != 64 {
		return fmt.Errorf("校验值必须是 64 位十六进制摘要")
	}
	for _, ch := range drill.Checksum {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return fmt.Errorf("校验值必须是 64 位十六进制摘要")
		}
	}
	if err := validBackupTarget(drill.TargetKind, drill.TargetRef); err != nil && !(drill.TargetKind == "local" && drill.TargetRef == "") {
		return err
	}
	_, err := db.pool.Exec(ctx, `INSERT INTO backup_drills(id,host_id,vm_name,target_kind,target_ref,checksum,result,note,actor_name)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		drill.ID, drill.HostID, drill.VMName, drill.TargetKind, drill.TargetRef, drill.Checksum, drill.Result, drill.Note, drill.ActorName)
	return err
}

func (db *Store) ListBackupDrills(ctx context.Context, hostID, vmName string) ([]BackupDrill, error) {
	rows, err := db.pool.Query(ctx, `SELECT id,host_id,vm_name,target_kind,target_ref,checksum,result,note,actor_name,created_at
		FROM backup_drills WHERE host_id=$1 AND vm_name=$2 ORDER BY created_at DESC LIMIT 20`, hostID, vmName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BackupDrill
	for rows.Next() {
		var item BackupDrill
		if err := rows.Scan(&item.ID, &item.HostID, &item.VMName, &item.TargetKind, &item.TargetRef, &item.Checksum, &item.Result, &item.Note, &item.ActorName, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
