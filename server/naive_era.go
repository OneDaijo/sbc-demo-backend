package main

type NaiveERA struct {
}

func (NaiveERA) predictProbDefault(borrower_app BorrowerApp) float64 {
	return 0.5 // Uniform prior
}

func (NaiveERA) predictInterestRate(prob_default float64) float64 {
	return prob_default * MAX_INTEREST_RATE // uses linear scaling
}

func (NaiveERA) computeQinCollateral(prob_default float64, num_successful_loans uint64) float64 {
	// The collateral is a linear scaling between the max and min collateral wrt probability of default
	// and based on the number of successful loans they have had, they need to post less collateral
	return prob_default * MAX_QIN_COLLATERAL * (1.0 / (float64(num_successful_loans) + 1.0))
}

func (NaiveERA) computeQinReward(prob_default float64, interest_reward float64) float64 {
	// The reward for the borrower is a fraction of the interest that ERA gets on successful repayment, rewarded for higher prob default
	// since those are the borrowers that need to put up higher collateral
	return prob_default * interest_reward // uses linear scaling
}

func (NaiveERA) rejectBorrower(prob_default float64) bool {
	return prob_default > 1.0 // naive never rejects
}
