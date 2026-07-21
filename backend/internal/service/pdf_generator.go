package service

import (
	"fmt"
	"strings"

	"github.com/genazt/my-budget-script/backend/internal/domain"
	"github.com/jung-kurt/gofpdf"
)

func formatGermanAmount(v float64) string {
	negative := v < 0
	if negative {
		v = -v
	}
	parts := strings.Split(fmt.Sprintf("%.2f", v), ".")
	intPart := parts[0]
	decPart := parts[1]

	var result []string
	for len(intPart) > 3 {
		result = append([]string{intPart[len(intPart)-3:]}, result...)
		intPart = intPart[:len(intPart)-3]
	}
	if len(intPart) > 0 {
		result = append([]string{intPart}, result...)
	}

	formatted := strings.Join(result, ".") + "," + decPart
	if negative {
		formatted = "-" + formatted
	}
	return formatted
}

func (s *ProjectionService) GenerateScenarioPDF(scenarioName string, months []domain.ProjectionMonth) ([]byte, error) {
	return GenerateScenarioPDF(scenarioName, months)
}

func GenerateScenarioPDF(scenarioName string, months []domain.ProjectionMonth) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetTitle(fmt.Sprintf("Projection Report - %s", scenarioName), true)
	pdf.SetAuthor("WealthEngine", true)

	for _, month := range months {
		pdf.AddPage()

		// 1. HEADER BANNER
		pdf.SetFillColor(248, 250, 252) // slate-50
		pdf.Rect(0, 0, 210, 35, "F")

		// Date Parse & Title
		label := month.Date.Format("January 2006")

		pdf.SetXY(12, 10)
		pdf.SetFont("Arial", "B", 16)
		pdf.SetTextColor(15, 23, 42) // slate-900
		pdf.Cell(100, 8, label)

		pdf.SetXY(12, 18)
		pdf.SetFont("Arial", "I", 9)
		pdf.SetTextColor(100, 116, 139) // slate-500
		pdf.Cell(100, 6, fmt.Sprintf("Scenario: %s", scenarioName))

		// 2. OVERVIEW SUMMARY GRID
		pdf.SetXY(12, 38)
		pdf.SetFont("Arial", "B", 10)
		pdf.SetTextColor(15, 23, 42)
		pdf.Cell(186, 6, "Monthly Summary Overview")
		pdf.Ln(6)

		drawSummaryCell := func(label string, val float64, isPositive bool) {
			pdf.SetFont("Arial", "B", 8)
			pdf.SetFillColor(241, 245, 249) // slate-100
			pdf.SetTextColor(71, 85, 105)   // slate-600
			pdf.CellFormat(23, 5.5, label, "1", 0, "L", true, 0, "")

			pdf.SetFont("Arial", "", 8)
			if isPositive {
				pdf.SetTextColor(22, 163, 74) // emerald-600
			} else {
				pdf.SetTextColor(225, 29, 72) // rose-600
			}
			valStr := "€ " + formatGermanAmount(val)
			pdf.CellFormat(23, 5.5, valStr, "1", 0, "R", false, 0, "")
		}

		drawSummaryCell("Income", month.Income, true)
		drawSummaryCell("Assets Out", month.Assets, false)
		drawSummaryCell("Loans Out", month.Loans, false)
		drawSummaryCell("Remainder", month.Remainder, month.Remainder >= 0)
		pdf.Ln(5.5)
		drawSummaryCell("Bills Out", month.Bills, false)
		drawSummaryCell("Expenses Out", month.Expenses, false)
		drawSummaryCell("Asset Worth", month.AssetWorth, true)
		drawSummaryCell("Loan Debt", month.LoanDebt, false)
		pdf.Ln(9)

		// 3. TABLES BREAKDOWN
		drawSectionTable := func(title string, headers []string, widths []float64, alignments []string, rows [][]string) {
			pdf.SetFont("Arial", "B", 9)
			pdf.SetTextColor(79, 70, 229) // Indigo-600
			pdf.Cell(186, 5, title)
			pdf.Ln(5)

			if len(rows) == 0 {
				pdf.SetFont("Arial", "I", 7.5)
				pdf.SetTextColor(148, 163, 184) // slate-400
				pdf.Cell(186, 4, "No entries recorded for this period.")
				pdf.Ln(6)
				return
			}

			// Headers
			pdf.SetFont("Arial", "B", 7.5)
			pdf.SetFillColor(241, 245, 249)
			pdf.SetTextColor(71, 85, 105)
			for i, h := range headers {
				pdf.CellFormat(widths[i], 5, h, "1", 0, alignments[i], true, 0, "")
			}
			pdf.Ln(5)

			// Rows
			pdf.SetFont("Arial", "", 7.5)
			pdf.SetTextColor(51, 65, 85) // slate-700
			for rIdx, r := range rows {
				fill := rIdx%2 == 1
				if fill {
					pdf.SetFillColor(248, 250, 252) // slate-50
				}
				for i, val := range r {
					pdf.CellFormat(widths[i], 4.5, val, "1", 0, alignments[i], fill, 0, "")
				}
				pdf.Ln(4.5)
			}
			pdf.Ln(4)
		}

		// Income Streams
		incomeRows := [][]string{}
		for _, inc := range month.Breakdown.Incomes {
			incomeRows = append(incomeRows, []string{inc.Name, "€ " + formatGermanAmount(inc.Amount)})
		}
		drawSectionTable("Income Streams", []string{"Source/Description", "Amount"}, []float64{130, 56}, []string{"L", "R"}, incomeRows)

		// Bills
		billRows := [][]string{}
		for _, b := range month.Breakdown.Bills {
			billRows = append(billRows, []string{b.Name, "€ " + formatGermanAmount(b.Amount)})
		}
		drawSectionTable("Fixed Bills & Subscriptions", []string{"Bill Name", "Amount"}, []float64{130, 56}, []string{"L", "R"}, billRows)

		// Expenses
		expenseRows := [][]string{}
		for _, exp := range month.Breakdown.Expenses {
			expenseRows = append(expenseRows, []string{exp.Name, "€ " + formatGermanAmount(exp.Amount)})
		}
		drawSectionTable("Discretionary Expenses", []string{"Expense Name", "Amount Due"}, []float64{130, 56}, []string{"L", "R"}, expenseRows)

		// Assets
		assetRows := [][]string{}
		for _, as := range month.Breakdown.Assets {
			assetRows = append(assetRows, []string{
				as.Name,
				"€ " + formatGermanAmount(as.Amount),
				"€ " + formatGermanAmount(as.Interest),
				"€ " + formatGermanAmount(as.Penalty),
				"€ " + formatGermanAmount(as.Balance),
			})
		}
		drawSectionTable("Assets & ETF Growth", []string{"Asset Name", "Contribution/Payout", "Interest/Yield", "Tax/Penalty Paid", "End Balance"}, []float64{70, 30, 26, 26, 34}, []string{"L", "R", "R", "R", "R"}, assetRows)

		// Loans
		loanRows := [][]string{}
		for _, l := range month.Breakdown.Loans {
			loanRows = append(loanRows, []string{
				l.Name,
				"€ " + formatGermanAmount(l.Amount),
				"€ " + formatGermanAmount(l.Balance),
			})
		}
		drawSectionTable("Loans & Repayments", []string{"Loan Name", "Amount Paid", "Remaining Debt Balance"}, []float64{90, 40, 56}, []string{"L", "R", "R"}, loanRows)

		// Virtual Accounts
		vaRows := [][]string{}
		for _, va := range month.VirtualAccounts {
			vaRows = append(vaRows, []string{
				va.Name,
				"€ " + formatGermanAmount(va.StartingBalance),
				"€ " + formatGermanAmount(va.Inflow),
				"€ " + formatGermanAmount(va.Outflow),
				"€ " + formatGermanAmount(va.Balance),
			})
		}
		drawSectionTable("Virtual Accounts / Envelopes", []string{"Account Name", "Starting Balance", "Allocated Bills", "Allocated Expenses", "Final Balance"}, []float64{66, 30, 30, 30, 30}, []string{"L", "R", "R", "R", "R"}, vaRows)
	}

	var buf strings.Builder
	err := pdf.Output(&buf)
	if err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}
