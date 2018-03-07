package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"golang.org/x/net/context"

	"cloud.google.com/go/datastore"
	firebase "firebase.google.com/go"
	"firebase.google.com/go/auth"
	b "github.com/stellar/go/build"
	"github.com/stellar/go/clients/horizon"
)

const kNumDbClients int64 = 5
const kUserKind string = "user"
const kLoanHistoryKind string = "loans"

var (
	ErrAuthFailed           = errors.New("Authentication failed.")
	ErrEmailNotValidated    = errors.New("Email has not yet been verified.")
	ErrUserDisabled         = errors.New("User account has been disabled.")
	ErrAuthTokenNotProvided = errors.New("Auth token not provided.")
	ErrUserNotFound         = errors.New("User was not found.")
	ErrLoanInWrongState     = errors.New("Active loan was not in the correct state for this request.")
	ErrNoActiveLoan         = errors.New("User has no active loan.")
	ErrInvalidId            = errors.New("Provided ID was not found.")
	ErrUserAlreadyExists    = errors.New("User already exists.")
	ErrUserNotRegistered    = errors.New("User not registered.")
	ErrLoanInDefault        = errors.New("Loan cannot be repaid as it is in default.")
	ErrNotEnoughQin         = errors.New("Not enough QIN.")
	ErrBadJsonPopulation    = errors.New("Some JSON fields were missing or populated incorrectly.")
	ErrLoanAlreadyExists    = errors.New("Active loan already exists.")
	ErrUserDataNotFound     = errors.New("Employment and residence information was not found for this user.")
)

type EmploymentInfo struct {
	EmploymentStatus     string   `json:"employmentStatus,omitempty"` // "EMPLOYED", "UNEMPLOYED", or "STUDENT"
	EmploymentJobTitle   string   `json:"employmentJobTitle,omitempty"`
	EmploymentStartMonth *int64   `json:"employmentStartMonth,omitempty"`
	EmploymentStartYear  *int64   `json:"employmentStartYear,omitempty"`
	EmploymentIncome     *float64 `json:"employmentIncome,omitempty"`
	EmploymentEducation  string   `json:"employmentEducation,omitempty"`
}

type ResidenceInfo struct {
	ResidenceAddr1    string   `json:"residenceAddr1,omitempty"`
	ResidenceAddr2    string   `json:"residenceAddr2,omitempty"`
	ResidenceDistrict string   `json:"residenceDistrict,omitempty"`
	ResidenceCity     string   `json:"residenceCity,omitempty"`
	ResidencePostal   string   `json:"residencePostal,omitempty"`
	ResidenceProvince string   `json:"residenceProvince,omitempty"`
	ResidenceStatus   string   `json:"residenceStatus,omitempty"` // “own” or “rent"
	ResidenceRentAmt  *float64 `json:"residenceRentAmt,omitempty"`
}

// The User Type (more like an object)
type User struct {
	Firstname       string  `json:"firstName,omitempty"`
	Lastname        string  `json:"lastName,omitempty"`
	PhoneNum        string  `json:"phoneNumber,omitempty"`
	DateOfBirth     string  `json:"dateOfBirth,omitempty"`
	QinBalance      float64 `json:"qinBalance"`
	DateCreated     int64   `json:"created"`
	*EmploymentInfo `json:"employmentInfo"`
	*ResidenceInfo  `json:"residenceInfo"`
}

type CreateUserResponse struct {
	Success bool `json:"success,omitempty"`
}

type LoanRequest struct {
	LoanAmount  float64 `json:"loanAmount"`
	LoanMemo    string  `json:"loanMemo,omitempty"`
	LoanPurpose string  `json:"loanPurpose,omitempty"`
	TermsAgreed bool    `json:"termsAgreed"`
	*User       `json:"user,omitempty"`
}

type LoanTerms struct {
	TermId       string  `json:"id,omitempty"`
	InterestRate float64 `json:"interestRate"`
	QinReward    float64 `json:"qinReward"`
	QinRequired  float64 `json:"qinRequired"`
	AmountOwed   float64 `json:"amountOwed"`
	OfferedBy    string  `json:"offeredBy,omitempty"`
}

type PickupLocation struct {
	LocationName string `json:"locationName,omitempty"`
}

type Repayment struct {
	Amount    float64 `json:"amount"`
	Timestamp int64   `json:"timestamp"`
}

type LoanRecord struct {
	LoanId        string          `json:"id,omitempty"`
	Amount        float64         `json:"amount"`
	CurrencyCode  string          `json:"currencyCode,omitempty"` // PHP
	DueDate       int64           `json:"dueDate,omitempty"`      // Unix milliseconds
	Terms         []LoanTerms     `json:"loanTerms,omitempty"`
	AcceptedTerms *LoanTerms      `json:"acceptedTerms,omitempty"`
	State         string          `json:"state,omitempty"`
	Location      *PickupLocation `json:"pickupLocation,omitempty"`
	Repayments    []Repayment     `json:"repayments,omitempty"`
	Memo          string          `json:"memo,omitempty"`
	Request       *LoanRequest    `json:"loanRequest,omitempty"`
	RepaidDate    int64           `json:"repaidDate,omitempty"`
	DateCreated   int64           `json:"created"`
}

type LoanHistory struct {
	LoanRecords []LoanRecord `json:"loans,omitempty"`
}

type LoanSelectRequest struct {
	SelectedTerm string         `json:"selectedTerm,omitempty"`
	Location     PickupLocation `json:"pickupLocation,omitempty"`
}

type LoanDeleteResponse struct {
	Success bool `json:"success"`
}

type FirebaseAuthRequest struct {
	Token string
}

type FirebaseAuthResponse struct {
	Success  bool
	Error    error
	UserInfo auth.UserInfo
}

type FederationResponse struct {
	AccountId string `json:"account_id,omitempty"`
	MemoType  string `json:"memoType,omitempty"`
	Memo      string `json:"memo,omitempty"`
}

// Account seed
var from *string

// ERA
var eraDriver *ERADriver

// Database Channels
var getDbClient chan *datastore.Client
var returnDbClient chan *datastore.Client
var dbDone chan bool

// Auth channels
var authRequests chan FirebaseAuthRequest
var authResponses chan FirebaseAuthResponse
var authDone chan bool

func Round(f float64) float64 {
	return float64(int(f + math.Copysign(0.5, f)))
}

func GetErrorCode(err error) int {
	switch err {
	case ErrAuthFailed:
		return http.StatusUnauthorized
	case ErrEmailNotValidated:
		return http.StatusBadRequest
	case ErrUserDisabled:
		return http.StatusBadRequest
	case ErrAuthTokenNotProvided:
		return http.StatusBadRequest
	case ErrUserNotFound:
		return http.StatusNotFound
	case ErrLoanInWrongState:
		return http.StatusBadRequest
	case ErrNoActiveLoan:
		return http.StatusNotFound
	case ErrInvalidId:
		return http.StatusNotFound
	case ErrUserAlreadyExists:
		return http.StatusConflict
	case ErrUserNotRegistered:
		return http.StatusNotFound
	case ErrLoanInDefault:
		return http.StatusBadRequest
	case datastore.ErrNoSuchEntity:
		return http.StatusNotFound
	case ErrNotEnoughQin:
		return http.StatusBadRequest
	case ErrBadJsonPopulation:
		return http.StatusBadRequest
	case ErrLoanAlreadyExists:
		return http.StatusConflict
	case ErrUserDataNotFound:
		return http.StatusNotFound
	default:
		// Log internal server errors.
		fmt.Println(err)
		return http.StatusInternalServerError
	}
}

func CheckOrigin(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	match, _ := regexp.MatchString(`\.onedaijo.com(?::\d+)?$`, origin)
	if match {
		w.Header().Add("Access-Control-Allow-Origin", origin)
		w.Header().Add("Access-Control-Allow-Credentials", "true") // Allow cookies to be used in requests
		w.Header().Add("Vary", "Origin")                           // use cached response based on origin
	}
}

func ManageDbClients() {
	// Intialize database connection
	ctx := context.Background()

	// Set your Google Cloud Platform project ID.
	projectID := "testfaketest-a6c57"

	for i := int64(0); i < kNumDbClients; i++ {
		// Creates a client.
		client, err := datastore.NewClient(ctx, projectID)
		if err != nil {
			log.Fatalf("Failed to create client: %v", err)
		}

		getDbClient <- client
	}

	for true {
		select {
		case clientToRecycle := <-returnDbClient:
			if clientToRecycle != nil {
				getDbClient <- clientToRecycle
			} else {
				// Creates new client since the old one was returned as nil because of a problem.
				client, err := datastore.NewClient(ctx, projectID)
				if err != nil {
					fmt.Printf("Failed to create client: %v", err)
				}
				getDbClient <- client
			}
		case <-dbDone:
			break
		}
	}
}

func Auth() {
	for true {
		// Pulls credentials from env var
		app, err := firebase.NewApp(context.Background(), nil)
		if err != nil {
			log.Fatalf("firebase app creation error")
		}
		client, err := app.Auth(context.Background())
		if err != nil {
			log.Fatalf("error getting Auth client")
		}
		// This is just custom token generation sample code for refernce.
		// token, err := client.CustomToken("8KvH0XdKOicatw4Fv5tnAONsCgl2")
		// if err != nil {
		//   fmt.Printf("error getting custom token")
		//   return
		// }
		// fmt.Printf("%s",token)
		select {
		case authRequest := <-authRequests:
			var response FirebaseAuthResponse
			tokenObj, err := client.VerifyIDToken(authRequest.Token)
			if err != nil {
				response.Success = false
				response.Error = ErrAuthFailed
			} else {
				userObj, err := client.GetUser(context.Background(), tokenObj.UID)
				if err != nil {
					response.Error = ErrAuthFailed
					response.Success = false
				} else {
					if userObj.Disabled {
						response.Success = false
						response.Error = ErrUserDisabled
					} else if !userObj.EmailVerified {
						response.Success = false
						response.Error = ErrEmailNotValidated
					} else {
						response.Success = true
					}
					response.UserInfo = *userObj.UserInfo
				}
			}
			authResponses <- response
		case <-authDone:
			break
		}
	}
}

func DoAuth(r *http.Request, requireEmailVerification bool) (FirebaseAuthResponse, error) {
	var authReq FirebaseAuthRequest
	token := r.Header.Get("X-firebase-token")
	if token == "" {
		return FirebaseAuthResponse{}, ErrAuthTokenNotProvided
	}
	authReq.Token = token
	authRequests <- authReq
	response := <-authResponses
	if !response.Success {
		if !requireEmailVerification && response.Error == ErrEmailNotValidated {
			return response, nil
		}
		return response, response.Error
	}
	return response, nil
}

func GetUser(w http.ResponseWriter, r *http.Request) {
	CheckOrigin(w, r)

	w.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	authResponse, err := DoAuth(r, false)

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	dbClient := <-getDbClient

	ctx := context.Background()
	userKey := datastore.NameKey(kUserKind, authResponse.UserInfo.UID, nil)
	var readUser *User

	_, err = dbClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {

		var user User

		get_err := tx.Get(userKey, &user)

		if get_err != nil {
			return get_err
		}

		readUser = &user

		return nil

	})

	returnDbClient <- dbClient

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	if readUser.EmploymentInfo == nil {
		readUser.EmploymentInfo = new(EmploymentInfo)
	}

	if readUser.ResidenceInfo == nil {
		readUser.ResidenceInfo = new(ResidenceInfo)
	}

	json.NewEncoder(w).Encode(readUser)
}

func CreateUser(w http.ResponseWriter, r *http.Request) {
	CheckOrigin(w, r)

	w.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	authResponse, err := DoAuth(r, false)

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	var user User
	err = json.NewDecoder(r.Body).Decode(&user)

	if err != nil || user.Firstname == "" || user.Lastname == "" || user.DateOfBirth == "" || user.PhoneNum == "" {
		err = ErrBadJsonPopulation
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	user.QinBalance = 0.0

	user.DateCreated = time.Now().Unix() * 1000

	dbClient := <-getDbClient

	ctx := context.Background()
	userKey := datastore.NameKey(kUserKind, authResponse.UserInfo.UID, nil)

	_, err = dbClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		var scratchUser User

		// This should fail because the user should not exist
		get_err := tx.Get(userKey, &scratchUser)
		if get_err == nil {
			return ErrUserAlreadyExists
		} else if get_err != datastore.ErrNoSuchEntity {
			return get_err
		}

		_, put_err := tx.Put(userKey, &user)
		if put_err != nil {
			return put_err
		}

		return nil

	})

	returnDbClient <- dbClient

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	if user.EmploymentInfo == nil {
		user.EmploymentInfo = new(EmploymentInfo)
	}

	if user.ResidenceInfo == nil {
		user.ResidenceInfo = new(ResidenceInfo)
	}

	json.NewEncoder(w).Encode(user)
}

func sendTransaction(tx *b.TransactionBuilder, from *string, hc *horizon.Client) error {

	txe, err := tx.Sign(*from)
	if err != nil {
		return err
	}

	txeB64, err := txe.Base64()

	if err != nil {
		return err
	}

	resp, err := hc.SubmitTransaction(fmt.Sprintf("%s", txeB64))
	if err != nil {
		fmt.Println(err)
		herr, isHorizonError := err.(*horizon.Error)
		if isHorizonError {
			resultCodes, err := herr.ResultCodes()
			if err != nil {
				fmt.Println("failed to extract result codes from horizon response")
				return err
			}
			fmt.Println(resultCodes)
		}
		return err
	}

	fmt.Println("transaction posted in ledger:", resp.Ledger)

	return nil

}

func GetBloomAddressAndMemo(c *http.Client) (string, string, error) {

	// Dummy federation call.
	r, err := c.Get("https://staging.bloomremit.net/stellar/federation?type=forward&forward_type=remittance_center&code=BOPIPHMM")

	defer r.Body.Close()

	if err != nil {
		return "", "", err
	}

	var federationResponse FederationResponse
	json.NewDecoder(r.Body).Decode(&federationResponse)

	// No compliance for the demo - users are not registered with bloom and we do not want to expose our demo users' personal info to a third party.

	return federationResponse.AccountId, federationResponse.Memo, nil
}

// For demo purposes, all calls will be dummy calls from here on in, the only value that matters is the amount.
func SendToBloom(amount float64) error {

	c := &http.Client{
		Timeout: 10 * time.Second,
	}

	address, memo, err := GetBloomAddressAndMemo(c)

	if err != nil {
		return err
	}

	hc := &horizon.Client{
		URL:  "https://horizon-testnet.stellar.org",
		HTTP: c,
	}

	tx, err := b.Transaction(
		b.SourceAccount{AddressOrSeed: *from},
		b.TestNetwork,
		b.AutoSequence{SequenceProvider: hc},
		b.Payment(
			b.Destination{AddressOrSeed: address},
			b.CreditAmount{"PHP", "GCBEJ5SNCV4B3E2TEDEUNR7DSC7Y4RLFAGSPNKZGNIOHQFWBHXCMMHZA", strconv.FormatFloat(amount, 'f', -1, 64)},
			b.PayWith(b.Asset{Native: true}, "1000000"),
		),
		b.MemoText{memo},
	)

	if err != nil {
		return err
	}

	err = sendTransaction(tx, from, hc)

	if err != nil {
		return err
	}

	return nil
}

func IsLoanActive(loanRecord *LoanRecord) (bool, error) {
	switch loanRecord.State {
	case "PENDING":
		return true, nil
	case "APPROVED":
		return true, nil
	case "REJECTED":
		return false, nil
	case "ACCEPTED":
		return true, nil
	case "SENT":
		return true, nil
	case "REPAID":
		return false, nil
	case "DEFAULTED":
		return false, nil
	case "CANCELED":
		return false, nil
	default:
		return false, errors.New("Invalid state value")
	}
}

func ActiveLoanForLoanHistory(loanHistory *LoanHistory) (*LoanRecord, error) {
	var loanRecord *LoanRecord
	var hasFoundLoan bool
	hasFoundLoan = false
	for i, loan := range loanHistory.LoanRecords {
		isActive, err := IsLoanActive(&loan)
		if err != nil {
			return nil, err
		}

		if isActive {
			if hasFoundLoan {
				return nil, errors.New("Found multiple active loans")
			}
			hasFoundLoan = true
			loanRecord = &loanHistory.LoanRecords[i]
		}
	}
	return loanRecord, nil
}

func DefaultActiveLoanIfNecessary(loanHistory *LoanHistory) (bool, error) {
	activeLoan, err := ActiveLoanForLoanHistory(loanHistory)
	if err != nil {
		return false, err
	}

	if activeLoan != nil && activeLoan.State == "SENT" {
		if activeLoan.DueDate == 0 {
			return false, errors.New("Due date not set for SENT loan")
		}
		var currentTime int64
		currentTime = time.Now().Unix() * 1000
		if activeLoan.DueDate < currentTime {
			activeLoan.State = "DEFAULTED"
			return true, nil
		}
	}

	// Default case, do nothing
	return false, nil
}

func LoanRequestFun(w http.ResponseWriter, r *http.Request) {
	CheckOrigin(w, r)

	w.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	authResponse, err := DoAuth(r, true)

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	var loanRecord *LoanRecord
	loanRecord = new(LoanRecord)
	loanRecord.Request = new(LoanRequest)
	err = json.NewDecoder(r.Body).Decode(loanRecord.Request)

	if err != nil || loanRecord.Request.User != nil || loanRecord.Request.LoanAmount == 0.0 {
		err = ErrBadJsonPopulation
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	loanRecord.Memo = loanRecord.Request.LoanMemo
	loanRecord.Amount = loanRecord.Request.LoanAmount
	loanRecord.CurrencyCode = "PHP"

	loanRecord.DateCreated = time.Now().Unix() * 1000

	// Note: this should be reset when the loan is accepted or rejected by the ERA.
	loanRecord.State = "PENDING"

	dbClient := <-getDbClient

	ctx := context.Background()
	loanHistoryKey := datastore.NameKey(kLoanHistoryKind, authResponse.UserInfo.UID, nil)
	userKey := datastore.NameKey(kUserKind, authResponse.UserInfo.UID, nil)
	var loanHistory *LoanHistory

	_, err = dbClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		loanHistory = new(LoanHistory)
		var user User

		get_err := tx.Get(userKey, &user)
		if get_err == datastore.ErrNoSuchEntity {
			return ErrUserNotRegistered
		} else if get_err != nil {
			return get_err
		}

		if user.EmploymentInfo == nil || user.ResidenceInfo == nil {
			return ErrUserDataNotFound
		}

		loanRecord.Request.User = &user

		get_err = tx.Get(loanHistoryKey, loanHistory)
		if get_err != nil && get_err != datastore.ErrNoSuchEntity {
			return get_err
		}

		// Don't need to know if it modified the active loan since a write will occur at the end of this func anyway.
		_, default_err := DefaultActiveLoanIfNecessary(loanHistory)

		if default_err != nil {
			return default_err
		}

		// Set loan ID
		numPrevLoans := len(loanHistory.LoanRecords)
		loanRecord.LoanId = authResponse.UserInfo.UID + "-" + strconv.Itoa(numPrevLoans)

		var borrowerInfo BorrowerInformation
		borrowerInfo.earned_qin = user.QinBalance
		borrowerInfo.no_loans = 0
		borrowerInfo.successful_loans = 0

		for _, loan := range loanHistory.LoanRecords {
			active, state_err := IsLoanActive(&loan)
			if state_err != nil {
				return state_err
			}
			if active {
				return ErrLoanAlreadyExists
			}

			if loan.State == "REPAID" {
				borrowerInfo.successful_loans++
				borrowerInfo.no_loans++
			}

			if loan.State == "DEFAULTED" {
				borrowerInfo.no_loans++
			}
		}

		var borrowerApp BorrowerApp
		borrowerApp.principal_amount = loanRecord.Amount
		borrowerApp.borrower_id = authResponse.UserInfo.UID

		// Handle all them pointers
		if income := loanRecord.Request.User.EmploymentInfo.EmploymentIncome; income == nil {
			borrowerApp.stated_monthly_income = 0
		} else {
			borrowerApp.stated_monthly_income = *income
		}

		if startMonth := loanRecord.Request.User.EmploymentInfo.EmploymentStartMonth; startMonth == nil {
			borrowerApp.employment_start_month = 0
		} else {
			borrowerApp.employment_start_month = *startMonth
		}

		if startYear := loanRecord.Request.User.EmploymentInfo.EmploymentStartYear; startYear == nil {
			borrowerApp.employment_start_year = 0
		} else {
			borrowerApp.employment_start_year = *startYear
		}

		borrowerApp.employment_status = loanRecord.Request.User.EmploymentInfo.EmploymentStatus

		// fmt.Printf("Borrower App Struct:\n%+v\n", &borrowerApp)
		// fmt.Printf("Borrower Info Struct:\n%+v\n", &borrowerInfo)

		era_terms, num_not_nil := processBorrowerRequest(eraDriver, borrowerApp, borrowerInfo)

		loanRecord.State = "APPROVED"

		if num_not_nil > 0 {
			loanRecord.State = "APPROVED"

			loanRecord.Terms = make([]LoanTerms, num_not_nil)

			var currentIndex int
			currentIndex = 0
			for _, terms := range era_terms {
				if terms != nil { // skip rejected eras
					// fmt.Printf("ERA Terms %i:\n%+v\n", i, terms)
					loanRecord.Terms[currentIndex].TermId = loanRecord.LoanId + "-" + strconv.Itoa(currentIndex)
					// Round to 4 decimal places (or round the percentage to 2 decimal places)
					loanRecord.Terms[currentIndex].InterestRate = Round(terms.interest_rate*10000.0) / 10000.0
					// Round QIN to nearest 0.01 QIN.
					loanRecord.Terms[currentIndex].QinReward = Round(terms.qin_reward*100.0) / 100.0
					loanRecord.Terms[currentIndex].QinRequired = Round(terms.qin_collateral*100.0) / 100.0
					// Round to the nearest $0.01
					loanRecord.Terms[currentIndex].AmountOwed = Round((1.0+loanRecord.Terms[currentIndex].InterestRate)*loanRecord.Amount*100.0) / 100.0
					loanRecord.Terms[currentIndex].OfferedBy = terms.offered_by
					currentIndex++
				}
			}

		} else {
			loanRecord.Terms = make([]LoanTerms, 1)

			loanRecord.Terms[0].TermId = loanRecord.LoanId + "-0"
			loanRecord.Terms[0].InterestRate = 0.05
			// Round QIN to nearest 0.01 QIN.
			loanRecord.Terms[0].QinReward = 0.1
			loanRecord.Terms[0].QinRequired = 0.0
			// Round to the nearest $0.01
			loanRecord.Terms[0].AmountOwed = Round((1.0+loanRecord.Terms[0].InterestRate)*loanRecord.Amount*100.0) / 100.0
			loanRecord.Terms[0].OfferedBy = "OneDaijo"
		}

		loanHistory.LoanRecords = append(loanHistory.LoanRecords, *loanRecord)

		_, put_err := tx.Put(loanHistoryKey, loanHistory)
		if put_err != nil {
			return put_err
		}

		return nil
	})

	returnDbClient <- dbClient

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	loanRecord.Request = nil
	json.NewEncoder(w).Encode(loanRecord)
}

func GetActiveLoan(w http.ResponseWriter, r *http.Request) {
	CheckOrigin(w, r)

	w.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	authResponse, err := DoAuth(r, true)

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	dbClient := <-getDbClient

	ctx := context.Background()
	loanHistoryKey := datastore.NameKey(kLoanHistoryKind, authResponse.UserInfo.UID, nil)
	var loanHistory *LoanHistory

	_, err = dbClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		loanHistory = new(LoanHistory)
		get_err := tx.Get(loanHistoryKey, loanHistory)
		if get_err != nil && get_err != datastore.ErrNoSuchEntity {
			return get_err
		}

		didModify, default_err := DefaultActiveLoanIfNecessary(loanHistory)

		if default_err != nil {
			return default_err
		}

		if didModify {
			_, put_err := tx.Put(loanHistoryKey, loanHistory)
			if put_err != nil {
				return put_err
			}
		}

		return nil
	})

	returnDbClient <- dbClient

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	activeLoan, err := ActiveLoanForLoanHistory(loanHistory)

	if err == nil && activeLoan == nil {
		err = ErrNoActiveLoan
	}

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	// Remove the request before encoding since that's not part of the API spec
	activeLoan.Request = nil

	json.NewEncoder(w).Encode(activeLoan)
}

func LoanTermsForId(termId string, loanRecord *LoanRecord) *LoanTerms {
	for i, terms := range loanRecord.Terms {
		if terms.TermId == termId {
			return &loanRecord.Terms[i]
		}
	}
	return nil
}

func SelectLoanOffer(w http.ResponseWriter, r *http.Request) {
	CheckOrigin(w, r)

	w.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	authResponse, err := DoAuth(r, true)

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	var loanSelectRequest LoanSelectRequest

	err = json.NewDecoder(r.Body).Decode(&loanSelectRequest)

	if err != nil || (loanSelectRequest.Location.LocationName == "" && loanSelectRequest.SelectedTerm == "") {
		err = ErrBadJsonPopulation
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	dbClient := <-getDbClient

	ctx := context.Background()
	loanHistoryKey := datastore.NameKey(kLoanHistoryKind, authResponse.UserInfo.UID, nil)
	userKey := datastore.NameKey(kUserKind, authResponse.UserInfo.UID, nil)
	var loanHistory *LoanHistory
	var activeLoan *LoanRecord

	_, err = dbClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		loanHistory = new(LoanHistory)
		var user User

		get_err := tx.Get(userKey, &user)
		if get_err == datastore.ErrNoSuchEntity {
			return ErrUserNotRegistered
		} else if get_err != nil {
			return get_err
		}

		get_err = tx.Get(loanHistoryKey, loanHistory)
		if get_err != nil && get_err != datastore.ErrNoSuchEntity {
			return get_err
		}

		activeLoan, err = ActiveLoanForLoanHistory(loanHistory)

		if err != nil {
			return err
		}

		if activeLoan == nil {
			return ErrNoActiveLoan
		}

		if activeLoan.State != "APPROVED" {
			return ErrLoanInWrongState
		}

		// Loan terms must be selected before or at the same time as pickup location
		if loanSelectRequest.SelectedTerm != "" {
			// Loan terms cannot be provided twice.
			if activeLoan.AcceptedTerms != nil {
				return ErrBadJsonPopulation
			}

			terms := LoanTermsForId(loanSelectRequest.SelectedTerm, activeLoan)
			if terms == nil {
				return ErrInvalidId
			}

			if terms.QinRequired > user.QinBalance {
				return ErrNotEnoughQin
			}

			activeLoan.AcceptedTerms = terms
		}

		if loanSelectRequest.Location.LocationName != "" {
			if activeLoan.AcceptedTerms == nil {
				return ErrBadJsonPopulation
			}

			activeLoan.Location = new(PickupLocation)
			*activeLoan.Location = loanSelectRequest.Location

			// Ignore money sending errors for the demo for demo
			// if err != nil {
			// 	return err
			// }

			// Adds 30 days, gets the unix timestamps rounds down to the nearest day and multiplies by 1000 to get it in milliseconds
			activeLoan.DueDate = (time.Now().AddDate(0, 0, 30).Unix() / 86400 * 86400) * 1000

			activeLoan.State = "SENT"

			if user.QinBalance < activeLoan.AcceptedTerms.QinRequired {
				return errors.New("Internal Error: user has less QIN than when loan was selected.")
			}

			user.QinBalance -= activeLoan.AcceptedTerms.QinRequired

			_, put_err := tx.Put(userKey, &user)
			if put_err != nil {
				return put_err
			}

		}

		_, put_err := tx.Put(loanHistoryKey, loanHistory)
		if put_err != nil {
			return put_err
		}

		return nil
	})

	returnDbClient <- dbClient

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	// Send to bloom outside of the transaction and ignore any error.
	err = SendToBloom(activeLoan.Amount)
	fmt.Println(err)

	activeLoan.Request = nil
	json.NewEncoder(w).Encode(activeLoan)
}

func Repay(w http.ResponseWriter, r *http.Request) {
	CheckOrigin(w, r)

	w.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	authResponse, err := DoAuth(r, true)

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	dbClient := <-getDbClient

	ctx := context.Background()
	loanHistoryKey := datastore.NameKey(kLoanHistoryKind, authResponse.UserInfo.UID, nil)
	userKey := datastore.NameKey(kUserKind, authResponse.UserInfo.UID, nil)
	var loanHistory *LoanHistory
	var activeLoan *LoanRecord

	var repaid bool

	_, err = dbClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		loanHistory = new(LoanHistory)
		var user User

		get_err := tx.Get(userKey, &user)
		if get_err == datastore.ErrNoSuchEntity {
			return ErrUserNotRegistered
		} else if get_err != nil {
			return get_err
		}

		get_err = tx.Get(loanHistoryKey, loanHistory)
		if get_err != nil && get_err != datastore.ErrNoSuchEntity {
			return get_err
		}

		activeLoan, err = ActiveLoanForLoanHistory(loanHistory)

		if err != nil {
			return err
		}

		if activeLoan == nil {
			return ErrNoActiveLoan
		}

		if activeLoan.State != "SENT" {
			return ErrLoanInWrongState
		}

		didModify, default_err := DefaultActiveLoanIfNecessary(loanHistory)

		if default_err != nil {
			return default_err
		}

		// If it was modified above, this loan is no longer active and should not be repaid
		if !didModify {
			var timestamp int64
			timestamp = time.Now().Unix() * 1000
			// Instant repayment for demo
			activeLoan.Repayments = append(activeLoan.Repayments, Repayment{Amount: activeLoan.AcceptedTerms.AmountOwed, Timestamp: timestamp})
			activeLoan.RepaidDate = timestamp
			activeLoan.State = "REPAID"

			// Return the collateral and give the reward
			user.QinBalance += activeLoan.AcceptedTerms.QinRequired + activeLoan.AcceptedTerms.QinReward

			// This is the only place where we modify user, so only write in this if
			_, put_err := tx.Put(userKey, &user)
			if put_err != nil {
				return put_err
			}

			repaid = true

		} else {
			repaid = false
		}

		_, put_err := tx.Put(loanHistoryKey, loanHistory)

		if put_err != nil {
			return put_err
		}

		return nil

	})

	returnDbClient <- dbClient

	if err == nil && !repaid {
		err = ErrLoanInDefault
	}

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	activeLoan.Request = nil
	json.NewEncoder(w).Encode(activeLoan)
}

func DeleteActiveLoan(w http.ResponseWriter, r *http.Request) {
	CheckOrigin(w, r)

	w.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	authResponse, err := DoAuth(r, true)

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	dbClient := <-getDbClient

	ctx := context.Background()
	loanHistoryKey := datastore.NameKey(kLoanHistoryKind, authResponse.UserInfo.UID, nil)
	var loanHistory *LoanHistory

	_, err = dbClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		loanHistory = new(LoanHistory)

		get_err := tx.Get(loanHistoryKey, loanHistory)
		if get_err != nil && get_err != datastore.ErrNoSuchEntity {
			return get_err
		}

		activeLoan, err := ActiveLoanForLoanHistory(loanHistory)

		if err != nil {
			return err
		}

		if activeLoan == nil {
			return ErrNoActiveLoan
		}

		if activeLoan.State != "APPROVED" && activeLoan.State != "PENDING" {
			return ErrLoanInWrongState
		}

		activeLoan.State = "CANCELED"

		_, put_err := tx.Put(loanHistoryKey, loanHistory)

		if put_err != nil {
			return put_err
		}

		return nil

	})

	returnDbClient <- dbClient

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	var loanDeleteResponse LoanDeleteResponse
	loanDeleteResponse.Success = true
	json.NewEncoder(w).Encode(loanDeleteResponse)
}

func GetLoans(w http.ResponseWriter, r *http.Request) {
	CheckOrigin(w, r)

	w.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	authResponse, err := DoAuth(r, true)

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	dbClient := <-getDbClient

	ctx := context.Background()
	loanHistoryKey := datastore.NameKey(kLoanHistoryKind, authResponse.UserInfo.UID, nil)
	var loanHistory *LoanHistory

	_, err = dbClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		loanHistory = new(LoanHistory)

		get_err := tx.Get(loanHistoryKey, loanHistory)
		if get_err != nil && get_err != datastore.ErrNoSuchEntity {
			return get_err
		}

		didModify, default_err := DefaultActiveLoanIfNecessary(loanHistory)

		if default_err != nil {
			return default_err
		}

		if didModify {
			_, put_err := tx.Put(loanHistoryKey, loanHistory)

			if put_err != nil {
				return put_err
			}
		}

		return nil

	})

	returnDbClient <- dbClient

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	for i := int(0); i < len(loanHistory.LoanRecords); i++ {
		// Remove the loan request because the client doesn't want that
		loanHistory.LoanRecords[i].Request = nil
	}

	json.NewEncoder(w).Encode(loanHistory)
}

func HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	dbClient := <-getDbClient
	returnDbClient <- dbClient
	var resp LoanDeleteResponse
	resp.Success = true
	json.NewEncoder(w).Encode(resp)
}

func HandleOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Add("Access-Control-Allow-Headers", "Content-type, X-firebase-token")

	CheckOrigin(w, r)
}

func PatchUser(w http.ResponseWriter, r *http.Request) {
	CheckOrigin(w, r)

	w.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	authResponse, err := DoAuth(r, true)

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	var user User
	err = json.NewDecoder(r.Body).Decode(&user)

	if err != nil || (user.EmploymentInfo == nil && user.ResidenceInfo == nil) {
		err = ErrBadJsonPopulation
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	var finalizedUser *User

	dbClient := <-getDbClient

	ctx := context.Background()
	userKey := datastore.NameKey(kUserKind, authResponse.UserInfo.UID, nil)

	_, err = dbClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		var existingUser User

		// The user must exist
		get_err := tx.Get(userKey, &existingUser)
		if get_err != nil {
			return get_err
		}

		if user.EmploymentInfo != nil {
			existingUser.EmploymentInfo = user.EmploymentInfo
		}

		if user.ResidenceInfo != nil {
			existingUser.ResidenceInfo = user.ResidenceInfo
		}

		_, put_err := tx.Put(userKey, &existingUser)
		if put_err != nil {
			return put_err
		}

		finalizedUser = &existingUser

		return nil

	})

	returnDbClient <- dbClient

	if err != nil {
		http.Error(w, err.Error(), GetErrorCode(err))
		return
	}

	if finalizedUser.EmploymentInfo == nil {
		finalizedUser.EmploymentInfo = new(EmploymentInfo)
	}

	if finalizedUser.ResidenceInfo == nil {
		finalizedUser.ResidenceInfo = new(ResidenceInfo)
	}

	json.NewEncoder(w).Encode(finalizedUser)
}

func main() {

	seed_bytes, err := ioutil.ReadFile("stellar_seed.txt")
	if err != nil {
		panic(err)
	}
	seed_string := string(seed_bytes)
	from = &seed_string

	// Constructing the ERA driver
	eraDriver = constructERADriver()

	// TODO(thiefinparis): update to a multi-client model like the db to increase parallelism
	// Firebase Channels
	authRequests = make(chan FirebaseAuthRequest)
	authResponses = make(chan FirebaseAuthResponse)
	authDone = make(chan bool)

	// Database channels
	getDbClient = make(chan *datastore.Client, kNumDbClients)
	returnDbClient = make(chan *datastore.Client, kNumDbClients)
	dbDone = make(chan bool)

	router := mux.NewRouter()
	router.HandleFunc("/user", HandleOptions).Methods("Options")
	router.HandleFunc("/loan-request", HandleOptions).Methods("Options")
	router.HandleFunc("/active-loan", HandleOptions).Methods("Options")
	router.HandleFunc("/repay", HandleOptions).Methods("Options")
	router.HandleFunc("/loans", HandleOptions).Methods("Options")
	router.HandleFunc("/hc", HandleOptions).Methods("Options")
	router.HandleFunc("/user", GetUser).Methods("Get")
	router.HandleFunc("/user", CreateUser).Methods("Post")
	router.HandleFunc("/user", PatchUser).Methods("Patch")
	router.HandleFunc("/loan-request", LoanRequestFun).Methods("Post")
	router.HandleFunc("/active-loan", GetActiveLoan).Methods("Get")
	router.HandleFunc("/active-loan", SelectLoanOffer).Methods("Put")
	router.HandleFunc("/active-loan", DeleteActiveLoan).Methods("Delete")
	router.HandleFunc("/repay", Repay).Methods("Post")
	router.HandleFunc("/loans", GetLoans).Methods("Get")
	router.HandleFunc("/hc", HealthCheck).Methods("Get")
	cfg := &tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		},
	}
	srv := &http.Server{
		Addr:         ":443",
		Handler:      router,
		TLSConfig:    cfg,
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0),
	}

	go Auth()
	go ManageDbClients()

	log.Fatal(srv.ListenAndServeTLS("server.crt", "server.key"))

	authDone <- true
	dbDone <- true
}
