package ocr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	aliocr "github.com/aliyun/alibaba-cloud-sdk-go/services/ocr"
)

// recognizedLine is one OCR text line with Alibaba's own confidence score.
type recognizedLine struct {
	text        string
	probability float64
}

// alibabaClient extracts the receipt total via Alibaba Cloud OCR.
// recognize is a seam so parsing can be tested without live credentials.
type alibabaClient struct {
	recognize func(context.Context, string) ([]recognizedLine, error)
}

func newAlibabaClient(cfg Config, logger *slog.Logger) Client {
	region := cfg.AlibabaRegionID
	if region == "" {
		region = "ap-southeast-1"
	}
	sdk, err := aliocr.NewClientWithAccessKey(region, cfg.AlibabaAccessKeyID, cfg.AlibabaAccessKeySecret)
	if err != nil {
		logger.Error("alibaba OCR client init failed", "error", err)
		return &alibabaClient{recognize: func(context.Context, string) ([]recognizedLine, error) {
			return nil, fmt.Errorf("alibaba OCR init: %w", err)
		}}
	}
	return &alibabaClient{recognize: func(ctx context.Context, imageRef string) ([]recognizedLine, error) {
		if !strings.HasPrefix(imageRef, "http://") && !strings.HasPrefix(imageRef, "https://") {
			return nil, fmt.Errorf("alibaba OCR needs a public image URL, got %q", imageRef)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req := aliocr.CreateRecognizeCharacterRequest()
		req.ImageURL = imageRef
		req.OutputProbability = requests.Boolean("true")
		resp, err := sdk.RecognizeCharacter(req)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, errors.New("alibaba OCR returned no response")
		}
		if resp.Data.ErrorCode != "" {
			return nil, fmt.Errorf("alibaba OCR %s: %s", resp.Data.ErrorCode, resp.Data.ErrorMessage)
		}
		lines := make([]recognizedLine, 0, len(resp.Data.Results))
		for _, r := range resp.Data.Results {
			lines = append(lines, recognizedLine{text: r.Text, probability: r.Probability})
		}
		return lines, nil
	}}
}

func (a *alibabaClient) Extract(ctx context.Context, imageRef string) (Result, error) {
	if imageRef == "" {
		return Result{}, fmt.Errorf("no receipt on file")
	}
	lines, err := a.recognize(ctx, imageRef)
	if err != nil {
		return Result{}, fmt.Errorf("alibaba OCR: %w", err)
	}
	return amountFromLines(lines)
}

var (
	totalLabel = regexp.MustCompile(`(?i)\b(grand\s*total|total|amount\s*due|net\s*amount|amount\s*payable)\b`)
	notTotal   = regexp.MustCompile(`(?i)\b(qty|quantity|balance)\b`)
	// shortcut: totals written as bare integers (e.g. "TOTAL 450") stay manual;
	// require a currency mark, decimal, or Indian comma grouping to avoid
	// reading a GST rate or quantity as the bill amount.
	moneyAmount = regexp.MustCompile(`(?i)(?:₹|Rs\.?|INR)\s*([0-9][0-9,]*(?:\.[0-9]{1,2})?)|([0-9]{1,3}(?:,[0-9]{2,3})+(?:\.[0-9]{1,2})?)|([0-9]+\.[0-9]{1,2})`)
)

// amountFromLines takes the last money-looking figure on the last labelled
// total line. No label or no money-looking figure → error, so the claim stays
// in manual review (Spec 22 §2.8 / parity plan §2.8).
func amountFromLines(lines []recognizedLine) (Result, error) {
	best := Result{}
	found := false
	for _, ln := range lines {
		if !totalLabel.MatchString(ln.text) || notTotal.MatchString(ln.text) {
			continue
		}
		amounts := amountsOnLine(ln.text)
		if len(amounts) == 0 {
			continue
		}
		if ln.probability < 0 || ln.probability > 1 {
			return Result{}, fmt.Errorf("alibaba OCR probability out of range: %f", ln.probability)
		}
		best = Result{Amount: amounts[len(amounts)-1], Confidence: ln.probability}
		found = true
	}
	if !found {
		return Result{}, errors.New("alibaba OCR: no labelled total amount on receipt")
	}
	return best, nil
}

func amountsOnLine(text string) []float64 {
	var out []float64
	for _, m := range moneyAmount.FindAllStringSubmatch(text, -1) {
		raw := m[1] + m[2] + m[3]
		if raw == "" {
			continue
		}
		amt, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", ""), 64)
		if err == nil && amt > 0 {
			out = append(out, amt)
		}
	}
	return out
}
