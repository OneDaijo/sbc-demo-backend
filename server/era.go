package main

// BorrowerApp represents the incoming borrower application for a loan request.
type BorrowerApp struct {
	borrower_id            string
	principal_amount       float64
	stated_monthly_income  float64
	employment_start_month int64
	employment_start_year  int64
	employment_status      string
}

const MAX_INTEREST_RATE float64 = 0.10 // Maximum interest rate that we are willing to set
const MAX_QIN_COLLATERAL float64 = 0.5 // Maximum collateral in terms of qin that a borrower is expected to have
const GRACE_NUM_LOANS uint64 = 1       // Number of loans where the borrower need not have any QIN

// BorrowerInformation represents the set of information used to determine the QIN collateral
type BorrowerInformation struct {
	no_loans         uint64
	successful_loans uint64
	earned_qin       float64
}

// ERATerms represents the terms of the ERA, containing both the interest rate, QIN collateral, and interest reward.
type ERATerms struct {
	interest_rate   float64
	qin_collateral  float64
	qin_reward      float64
	interest_reward float64
	offered_by      string
}

// ERA represents the external risk assessor who is responsible for approving/rejecting a loan and setting the interest rate and the QIN collateral
type ERA interface {
	predictProbDefault(borrower_app BorrowerApp) float64
	predictInterestRate(prob_default float64) float64
	computeQinCollateral(prob_default float64, successful_loans uint64) float64
	computeQinReward(prob_default float64, interest_reward float64) float64
	rejectBorrower(prob_default float64) bool
}

// Computes the fraction of interest that the ERA gets as reward given the fraction, interest rate, and loan principal
func computeInterestReward(fraction float64, interest_rate float64, loan_principal float64) float64 {
	return fraction * interest_rate * loan_principal
}

// Processes borrower application given borrower information to determine the interest rate, qin collateral, and qin reward
func processBorrowerApp(era ERA, borrower_app BorrowerApp, borrower_information BorrowerInformation, loan_fraction float64, offered_by string) *ERATerms {
	// Probability of default given the borrower's app
	prob_default := era.predictProbDefault(borrower_app)

	// Shamelessly fixing rather than throwing or enforcing back to ERA
	if prob_default < 0.0 {
		prob_default = 0.0
	} else if prob_default > 1.0 {
		prob_default = 1.0
	} else { // if between 0 and 1, then take no action

	}

	// Check if the borrower should be rejected based on default probability
	if era.rejectBorrower(prob_default) {
		return nil
	}

	// Check if borrower should be rejected on the basis on not having enough earned qin, short circuit otherwise
	// Qin collateral that the borrower must post given the borrower information
	qin_collateral := 0.0
	if borrower_information.no_loans >= GRACE_NUM_LOANS { // must have at least grace num loans for qin collateral to apply
		qin_collateral = era.computeQinCollateral(prob_default, borrower_information.successful_loans)
		if borrower_information.earned_qin < qin_collateral {
			return nil
		}
	}

	// Interest rate that is computed with the ERA logic given the borrower app
	interest_rate := era.predictInterestRate(prob_default)

	// Shamelessly fixing rather than throwing or enforcing back to ERA
	if interest_rate < 0.0 {
		interest_rate = 0.0
	} else if interest_rate > MAX_INTEREST_RATE {
		interest_rate = MAX_INTEREST_RATE
	} else { // if between 0 and MAX_INTEREST_RATE, then take no action

	}

	// Qin reward that the borrower gets at most given the borrower information
	interest_reward := loan_fraction * interest_rate
	qin_reward := era.computeQinReward(prob_default, interest_reward)

	// Runtime assertions to ensure that interest_rate, qin_collateral, qin_reward respect constraints
	// Shamelessly fixing rather than throwing or enforcing back to ERA
	if qin_reward < 0.0 {
		qin_reward = 0.0
	} else { // if greater than 0 then take no action

	}

	era_terms := ERATerms{interest_rate: interest_rate, qin_collateral: qin_collateral, qin_reward: qin_reward, interest_reward: interest_reward, offered_by: offered_by}
	return &era_terms // safe in go due to pointer escape analysis
}
