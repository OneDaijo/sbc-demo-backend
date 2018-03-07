package main

type ProsperERA struct {
}

const NUM_COEFFICIENTS = 3

func (ProsperERA) featureEngineering(borrower_app BorrowerApp) []float64 {
	features := make([]float64, NUM_COEFFICIENTS)
	features = append(features, 1.0, borrower_app.principal_amount, borrower_app.stated_monthly_income)
	return features
}

func (ProsperERA) predictProbDefault(borrower_app BorrowerApp) float64 {
	// Extracting coefficients for the SLERM
	coefficients := func() []float64 {
		PROSPER_SLERM_COEFFICIENTS := make([]float64, NUM_COEFFICIENTS)
		PROSPER_SLERM_COEFFICIENTS = append(PROSPER_SLERM_COEFFICIENTS, 0.0853254321972573, 1.91903762849344e-05, -2.81892322338568e-05)
		return PROSPER_SLERM_COEFFICIENTS
	}()

	// Extract features from input borrower app
	features := func(borrower_app BorrowerApp) []float64 {
		features := make([]float64, NUM_COEFFICIENTS)
		features = append(features, 1.0, borrower_app.principal_amount, borrower_app.stated_monthly_income)
		return features
	}(borrower_app)

	// output probability from trained SLERM (PLR)
	prob_default := featureToProb(coefficients, features)

	return prob_default
}

func (ProsperERA) predictInterestRate(prob_default float64) float64 {
	return prob_default * MAX_INTEREST_RATE // uses linear scaling
}

func (ProsperERA) computeQinCollateral(prob_default float64, num_successful_loans uint64) float64 {
	// The collateral is a linear scaling between the max and min collateral wrt probability of default
	// and based on the number of successful loans they have had, they need to post less collateral
	return prob_default * MAX_QIN_COLLATERAL * (1.0 / (float64(num_successful_loans) + 1.0))
}

func (ProsperERA) computeQinReward(prob_default float64, interest_reward float64) float64 {
	// The reward for the borrower is a fraction of the interest that ERA gets on successful repayment, rewarded for higher prob default
	// since those are the borrowers that need to put up higher collateral
	return prob_default * interest_reward // uses linear scaling
}

func (ProsperERA) rejectBorrower(prob_default float64) bool {
	return prob_default > 0.6
}
