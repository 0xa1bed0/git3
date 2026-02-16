package s3

import "encoding/xml"

// S3 XML types

type ListBucketResult struct {
	XMLName               xml.Name     `xml:"ListBucketResult"`
	Xmlns                 string       `xml:"xmlns,attr"`
	Name                  string       `xml:"Name"`
	Prefix                string       `xml:"Prefix"`
	KeyCount              int          `xml:"KeyCount"`
	MaxKeys               int          `xml:"MaxKeys"`
	IsTruncated           bool         `xml:"IsTruncated"`
	Contents              []ObjectInfo `xml:"Contents"`
	ContinuationToken     string       `xml:"ContinuationToken,omitempty"`
	NextContinuationToken string       `xml:"NextContinuationToken,omitempty"`
}

type ObjectInfo struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

type ErrorResponse struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}
