package utils

import (
	"errors"
	"strings"
)

const (
	arnDelimiter = ":"
	arnSections  = 5
	arnPrefix    = "acs:"

	// zero-indexed
	sectionPartition = 0
	sectionService   = 1
	sectionRegion    = 2
	sectionAccountID = 3
	sectionResource  = 4

	// errors
	invalidPrefix   = "acs: invalid prefix"
	invalidSections = "acs: not enough sections"
)

// ARN captures the individual fields of an Alibaba Cloud Resource Name.
type ARN struct {
	Partition string

	Service string

	Region string

	AccountID string

	Resource string
}

// Parse parses an ARN into its constituent parts.
//
// Some example ARNs:
// acs:ram::123456789012:user/tester
// acs:ram::123456789012:role/defaultrole
func ParseARN(arn string) (ARN, error) {
	if !strings.HasPrefix(arn, arnPrefix) {
		return ARN{}, errors.New(invalidPrefix)
	}
	sections := strings.SplitN(arn, arnDelimiter, arnSections)
	if len(sections) != arnSections {
		return ARN{}, errors.New(invalidSections)
	}
	return ARN{
		Partition: sections[sectionPartition],
		Service:   sections[sectionService],
		Region:    sections[sectionRegion],
		AccountID: sections[sectionAccountID],
		Resource:  sections[sectionResource],
	}, nil
}

// String returns the canonical representation of the ARN, only for testing
func (arn ARN) string() string {
	if arn.Partition == "" {
		return ""
	}
	return arn.Partition + arnDelimiter +
		arn.Service + arnDelimiter +
		arn.Region + arnDelimiter +
		arn.AccountID + arnDelimiter +
		arn.Resource
}
