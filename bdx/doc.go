// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package bdx is the Bulk Data Exchange protocol (Matter Core §11.22): the
// message codecs, a [Messenger] over one exchange, and the transfer roles —
// the initiating receiver an OTA Software Update Requestor runs to download
// an image ([Receive]), the initiating sender a DiagnosticLogs server runs
// to hand over its logs ([Send], [StartSend]), and the responder an OTA
// Software Update Provider serves its images with ([Responder], [Serve]).
//
// It is a port of matter.js packages/protocol/src/bdx (ADR 0019, ADR 0021):
//
//	matter.js                                    go-fabric
//	schema/BdxInitMessagesSchema.ts              Init, TransferControl
//	schema/BdxAcceptMessagesSchema.ts            SendAccept, ReceiveAccept
//	schema/BdxBlockMessagesSchema.ts             Block, CounterOnly, BlockQueryWithSkip
//	schema/BdxStatusMessageSchema.ts             EncodeStatusReport, DecodeStatusReport
//	schema/BdxMessage.ts                         Decode
//	BdxError.ts                                  Error, StatusResponseError
//	BdxMessenger.ts                              Messenger, TransferIdleTimeout
//	BdxSessionConfiguration.ts                   ReceiveOptions, SendOptions, ServeOptions, ScopeConfig
//	BdxClient.asReceiver / BdxSession            Receive
//	BdxClient.asSender / BdxSession              Send, StartSend, Transfer
//	BdxSession.fromMessage                       Serve
//	BdxProtocol.ts                               Responder, ServerSession
//	bdx-session-initiator.ts                     Receive, Send (initiator), Serve (responder)
//	flow/Flow.ts, flow/InboundFlow.ts            inboundFlow
//	flow/OutboundFlow.ts                         outboundFlow
//	flow/DrivingReceivingFlow.ts                 receiver drive, this node receiving
//	flow/FollowingSendingFlow.ts                 sender drive, this node receiving
//	flow/DrivenSendingFlow.ts                    sender drive, this node sending
//	flow/FollowingReceivingFlow.ts               receiver drive, this node sending
//	FileDesignator.ts                            FileDesignator, ParseBdxURI
//	PersistedFileDesignator.openBlob             Blob, Storage
//	PersistedFileDesignator.writeFromStream      Sink, FileSink, SinkStorage
//
// Every message of the protocol encodes and decodes, and every role but
// the asynchronous transfer — provisional, refused as matter.js refuses it —
// is ported.
//
// The package runs over an [Exchange], not over a transport: the bridge
// supplies one on a CASE session over MRP today, and a TCP connection can
// supply one without a change here.
package bdx
