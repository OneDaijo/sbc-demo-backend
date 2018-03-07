package main

type KivaERA struct {
}

func (KivaERA) predictProbDefault(borrower_app BorrowerApp) float64 {
	// Extracting coefficients for the SLERM
	coefficients := func() []float64 {
		KIVA_SLERM_COEFFICIENTS := make([]float64, 2)
		KIVA_SLERM_COEFFICIENTS = append(KIVA_SLERM_COEFFICIENTS, -3.92992243318379, 0.0018010882525536)
		return KIVA_SLERM_COEFFICIENTS
	}()

	// Extract features from input borrower app
	features := func(borrower_app BorrowerApp) []float64 {
		features := make([]float64, 2)
		features = append(features, 1.0, borrower_app.principal_amount)
		return features
	}(borrower_app)

	// output probability from trained SLERM (PLR)
	prob_default := featureToProb(coefficients, features)

	return prob_default
}

func (KivaERA) predictInterestRate(prob_default float64) float64 {
	return prob_default * MAX_INTEREST_RATE // uses linear scaling
}

func (KivaERA) computeQinCollateral(prob_default float64, num_successful_loans uint64) float64 {
	// The collateral is a linear scaling between the max and min collateral wrt probability of default
	// and based on the number of successful loans they have had, they need to post less collateral
	return prob_default * MAX_QIN_COLLATERAL * (1.0 / (float64(num_successful_loans) + 1.0))
}

func (KivaERA) computeQinReward(prob_default float64, interest_reward float64) float64 {
	// The reward for the borrower is a fraction of the interest that ERA gets on successful repayment, rewarded for higher prob default
	// since those are the borrowers that need to put up higher collateral
	return prob_default * interest_reward // uses linear scaling
}

func (KivaERA) rejectBorrower(prob_default float64) bool {
	return prob_default > 0.9
}
