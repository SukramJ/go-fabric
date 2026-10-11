// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package bdx is the Bulk Data Exchange protocol (Matter Core §11.22): the
// message codecs, a [Messenger] over one exchange, and the inbound transfer
// an OTA Software Update Requestor runs to download an image ([Receive]).
//
// It is a port of matter.js packages/protocol/src/bdx (ADR 0019):
//
//	matter.js                                    go-fabric
//	schema/BdxInitMessagesSchema.ts              Init, TransferControl
//	schema/BdxAcceptMessagesSchema.ts            SendAccept, ReceiveAccept
//	schema/BdxBlockMessagesSchema.ts             Block, CounterOnly, BlockQueryWithSkip
//	schema/BdxStatusMessageSchema.ts             EncodeStatusReport, DecodeStatusReport
//	schema/BdxMessage.ts                         Decode
//	BdxError.ts                                  Error, StatusResponseError
//	BdxMessenger.ts                              Messenger, TransferIdleTimeout
//	BdxClient.asReceiver / BdxSession            Receive
//	bdx-session-initiator.ts (receiver branch)   Receive
//	flow/DrivingReceivingFlow.ts                 Receive, receiver drive
//	flow/FollowingSendingFlow.ts                 Receive, sender drive
//	FileDesignator.ts                            FileDesignator, ParseBdxURI
//	PersistedFileDesignator.writeFromStream      Sink, FileSink
//
// Every message of the protocol encodes and decodes, the sender's included;
// the transfer roles ported are the ones the requestor needs: the initiating
// receiver, in both driver modes. The sending and responding roles
// (BdxProtocol, the outbound flows) belong to an OTA Provider, which this
// module does not expose (BD-Matter-OTAProvider-NotExposed).
//
// The package runs over an [Exchange], not over a transport: the bridge
// supplies one on a CASE session over MRP today, and a TCP connection can
// supply one without a change here.
package bdx
