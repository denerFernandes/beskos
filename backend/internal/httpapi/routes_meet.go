package httpapi

// Rotas do Meet: salas e reuniões, controlos do organizador, gravação, apresentação e lobby.
// Saíram da tabela de rotas por serem o maior bloco de um só produto (lei 1.4).

import "github.com/go-chi/chi/v5"

// routesMeet regista as rotas do Meet no grupo autenticado (sessão + CSRF já aplicados).
func (s *Server) routesMeet(r chi.Router) {
	// Meet (LiveKit): salas, reuniões agendadas, RSVP.
	r.Post("/meet/join", s.handleJoinMeet)
	r.Get("/meet/meetings", s.handleListMeetings)
	r.Post("/meet/meetings", s.handleCreateMeeting)
	r.Post("/meet/meetings/{id}/rsvp", s.handleRSVP)
	r.Post("/meet/meetings/{id}/cancel", s.handleCancelMeeting)
	r.Patch("/meet/meetings/{id}", s.handleUpdateMeeting) // editar/reagendar
	// Controles do organizador (a própria query filtra owner_user_id → só o dono altera).
	r.Patch("/meet/meetings/{id}/controls", s.handleUpdateMeetControls)
	// v1.5: quem está na sala, expulsar, e as notas por e-mail. Só o dono nos dois últimos.
	r.Get("/meet/meetings/{id}/participants", s.handleMeetParticipants)
	r.Post("/meet/meetings/{id}/kick", s.handleKickParticipant)
	r.Post("/meet/meetings/{id}/mute", s.handleMuteParticipant)
	r.Get("/meet/meetings/{id}/cohosts", s.handleListCohosts)                    // co-hosts da sala
	r.Post("/meet/meetings/{id}/cohosts", s.handleAddCohost)                     // promover (dono)
	r.Delete("/meet/meetings/{id}/cohosts/{uid}", s.handleRemoveCohost)          // despromover (dono)
	r.Get("/meet/meetings/{id}/breakouts", s.handleListBreakouts)                // salas de breakout
	r.Post("/meet/meetings/{id}/breakouts", s.handleCreateBreakouts)             // criar (host)
	r.Delete("/meet/meetings/{id}/breakouts", s.handleCloseBreakouts)            // fechar todas (host)
	r.Post("/meet/meetings/{id}/breakouts/{bid}/assign", s.handleAssignBreakout) // atribuir (host)
	r.Get("/meet/meetings/{id}/breakouts/{bid}/token", s.handleBreakoutToken)    // entrar num breakout
	r.Post("/meet/meetings/{id}/notes/email", s.handleSendMeetNotes)
	r.Patch("/meet/meetings/{id}/access", s.handleUpdateMeetAccess)
	// Notas da reunião (viram tarefas via IA no Kanban).
	r.Get("/meet/meetings/{id}/notes", s.handleGetMeetNotes)
	r.Put("/meet/meetings/{id}/notes", s.handleSaveMeetNotes)

	// Gravação (LiveKit Egress → MinIO). Opcional.
	r.Post("/meet/record/start", s.handleStartRecording)
	r.Post("/meet/record/stop", s.handleStopRecording)
	r.Get("/meet/recordings", s.handleListRecordings)
	r.Get("/meet/recordings/{id}/play", s.handlePlayRecording)
	r.Delete("/meet/recordings/{id}", s.handleDeleteRecording)

	// Meet: apresentação (estilo BBB). Iniciar por upload ou do Drive; slide corrente; parar.
	r.Post("/meet/presentation/upload", s.handleStartPresentationUpload)
	r.Post("/meet/presentation/drive", s.handleStartPresentationDrive)
	r.Get("/meet/presentation", s.handleGetPresentation)
	r.Post("/meet/presentation/{id}/slide", s.handleSetPresentationSlide)
	r.Post("/meet/presentation/{id}/stop", s.handleStopPresentation)

	// Meet: lobby (aprovação de convidados) — só o host.
	r.Get("/meet/meetings/{id}/lobby", s.handleListLobby)
	r.Post("/meet/lobby/{id}/admit", s.handleAdmit)
	r.Post("/meet/lobby/{id}/deny", s.handleDeny)
}
