package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxInvoiceFileSize = 20 * 1024 * 1024

func invoiceFileType(content []byte) (string, string, error) {
	if len(content) == 0 || len(content) > MaxInvoiceFileSize {
		return "", "", invoiceBad("发票文件不能为空且不能超过20MB")
	}
	kind := http.DetectContentType(content)
	switch kind {
	case "application/pdf":
		if !bytes.Contains(content, []byte("%%EOF")) {
			return "", "", invoiceBad("PDF文件不完整")
		}
		return kind, ".pdf", nil
	case "image/png":
		return kind, ".png", nil
	case "image/jpeg":
		return kind, ".jpg", nil
	default:
		return "", "", invoiceBad("只支持PDF、PNG、JPG发票文件")
	}
}

func (s *InvoiceService) Upload(ctx context.Context, id string, actor int64, content []byte, number string, confirmExternalChecked bool) (*InvoiceRecord, error) {
	kind, ext, err := invoiceFileType(content)
	if err != nil {
		return nil, err
	}
	number = strings.TrimSpace(number)
	if utf8.RuneCountInString(number) > 100 {
		return nil, invoiceBad("发票号码最多100个字符")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	r, err := scanInvoice(tx.QueryRowContext(ctx, invoiceSelect+` WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return nil, err
	}
	legacy := r.RequiresManualCheck
	allowed := r.Status == InvoiceApproved || legacy && slices.Contains([]string{InvoiceSubmitting, InvoiceProcessing, InvoiceUnknown, InvoiceAttention, InvoiceCancelling, InvoiceCompleted}, r.Status)
	if !allowed || r.Downloadable {
		return nil, invoiceConflict("请先审核通过；已上传的发票不能重复覆盖")
	}
	if legacy && !confirmExternalChecked {
		return nil, invoiceBad("该申请曾提交外部平台，请先核实开票结果并确认上传的是原申请发票")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO invoice_files(application_id,content,mime_type,file_name) VALUES($1,$2,$3,$4)`, id, content, kind, id+ext)
	if err != nil {
		return nil, invoiceConflict("发票文件已存在或保存失败，请刷新后核对")
	}
	now := time.Now()
	r.Manual = true
	r.Status = InvoiceCompleted
	r.Downloadable = true
	r.RequiresManualCheck = false
	r.InvoiceNumber = number
	r.UploadedBy = actor
	r.CompletedAt = &now
	r.LastError = ""
	r.ErrorCode = ""
	if err = saveInvoice(ctx, tx, r, strconv.FormatInt(actor, 10), "UPLOAD"); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.get(ctx, id, 0)
}

func (s *InvoiceService) Download(ctx context.Context, id string, user int64) ([]byte, string, string, error) {
	r, err := s.get(ctx, id, user)
	if err != nil {
		return nil, "", "", err
	}
	if r.Status != InvoiceCompleted || !r.Downloadable {
		return nil, "", "", invoiceConflict("发票尚未上传，请等待管理员处理")
	}
	var body []byte
	var kind, name string
	err = s.db.QueryRowContext(ctx, `SELECT content,mime_type,file_name FROM invoice_files WHERE application_id=$1`, id).Scan(&body, &kind, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", "", invoiceConflict("发票文件不存在，请联系管理员")
	}
	return body, kind, name, err
}
