package main

// Internal mapping of the ERAs for indices
type ERAIdx uint

const (
	kKiva    ERAIdx = 0
	kProsper ERAIdx = 1
	kNaive   ERAIdx = 2
	kRandom  ERAIdx = 3
)

// Loan status enumeration
type LoanStatus uint

const (
	kDefaulted LoanStatus = 0
	kPaid      LoanStatus = 1
)

// ERABalanceState represents the state of the reward for the ERA
type ERABalanceState struct {
	era_id          ERAIdx
	qin_reward      float64
	interest_reward float64
}

// LoanStatusState represents the status of a loan
type LoanStatusState struct {
	borrower_id string
	loan_status LoanStatus
}

// Initial QIN starting balance of the ERAs
const INITIAL_QIN_BALANCE float64 = 100.0
const ERA_INTEREST_FRACTION float64 = 0.02

// ERA driver represents the pseudo-object responsible for disseminating information to the ERAs and aggregating responses
type ERADriver struct {
	_eras                     []ERA                      // array of era structs
	_era_external_names       []string                   // array of corresponding era names TODO: can construct map instead
	_eras_qin_balances        []float64                  // array of era qin balances TODO: offload storing this state to database
	_eras_fiat_balances       []float64                  // array of era fiat balances TODO: offload storing this state to database
	_eras_borrower_assignment map[string]ERABalanceState // map: borrower_id -> eraBalanceState, borrower is sufficient, no need for loan level resolution right now

	_num_eras int // number of eras
}

// Acting Ctor for ERADriver
func constructERADriver() *ERADriver {
	era_driver := new(ERADriver)

	// Constructing individual ERAs
	// TODO Create registry service so we know what ERAs exist
	era_driver._eras = []ERA{KivaERA{}, ProsperERA{}, NaiveERA{}, RandomERA{}}
	era_driver._era_external_names = []string{"LendingData", "IntelligentAnalytica", "ABC Analytica", "Star Labs"}
	era_driver._num_eras = len(era_driver._eras)

	// Setting the initial qin balances of all the ERAs
	era_driver._eras_qin_balances = []float64{INITIAL_QIN_BALANCE, INITIAL_QIN_BALANCE, INITIAL_QIN_BALANCE, INITIAL_QIN_BALANCE}

	return era_driver // safe from pointer scope analysis
}

// Processes borrower request by mapping across each era and reducing over each of the responses
func processBorrowerRequest(era_driver *ERADriver, borrower_app BorrowerApp, borrower_information BorrowerInformation) ([]*ERATerms, uint) {
	// Initializing array for the output era terms
	era_responses := make([]*ERATerms, era_driver._num_eras, era_driver._num_eras)

	// Computing loan fraction that ERA gets as reward based on successful repayment of borrower
	var loan_fraction float64 = ERA_INTEREST_FRACTION * float64(borrower_app.principal_amount)

	// Generating responses for each individual borrower sequentially
	var num_not_nil uint = 0
	for i := 0; i < len(era_responses); i++ {
		era_responses[i] = processBorrowerApp(era_driver._eras[i], borrower_app, borrower_information, loan_fraction, era_driver._era_external_names[i])
		if era_responses[i] != nil {
			num_not_nil++
		}
	}

	return era_responses, num_not_nil
}

func (ERADriver) processLoanChoice(era_driver *ERADriver, borrower_id string, era_choice ERAIdx, qin_reward float64, interest_reward float64) {
	// ERA Balance State
	era_balance_state := ERABalanceState{era_id: era_choice, qin_reward: qin_reward, interest_reward: interest_reward}

	// ERA balance state to be set when the borrower
	era_driver._eras_borrower_assignment[borrower_id] = era_balance_state
}

// Processes loan status by updating the qin and fiat balance state of the ERA that the borrower's loan was atatched to
func (ERADriver) processLoanStatus(era_driver *ERADriver, loan_status_state LoanStatusState) {
	// Extracting balance state given the borrower id
	era_balance_state := era_driver._eras_borrower_assignment[loan_status_state.borrower_id]

	// If loan was paid back, qin reward goes to the borrower, else it will go to the lender
	era_driver._eras_qin_balances[era_balance_state.era_id] -= -era_balance_state.qin_reward

	switch loan_status_state.loan_status {
	case kPaid:
		era_driver._eras_fiat_balances[era_balance_state.era_id] += era_balance_state.interest_reward
	case kDefaulted:
		// No additional work needs to be done here
	}
}
