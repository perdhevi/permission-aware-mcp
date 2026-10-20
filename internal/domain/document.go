package domain

type Source struct {
	DocumentID string `json:"document_id"`
	Title      string `json:"title"`
}

type Document struct {
	DocumentID string `json:"document_id"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	Truncated  bool   `json:"truncated"`
}
