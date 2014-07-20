<%@ page language="java" contentType="text/html; charset=UTF-8"
    pageEncoding="UTF-8"%>
<html>
<head>
		<title>Alex's Nexus</title>
		<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
		<script type='text/javascript' src='<%=request.getContextPath()%>/js/jquery.js'></script>		
		<script type='text/javascript' src='<%=request.getContextPath()%>/js/bootstrap.js'></script>
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/bootstrap.min.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/bootstrap-responsive.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/typeplate-unminified.css' />
		<link rel='stylesheet' type='text/css' href='<%=request.getContextPath()%>/css/default.css' />
</head>
<body style = "background: none !important;">
<div class="messageBox">
	<div class="messageTitle errorTitle">
		<h5 class="epsilon">SYSTEM ALERT</h5>
	</div>
	<div class="innerMessage">
		<table>
			<tr>
				<td>
					<img class="message-image" src="img/permission_denied.png" height="130px">
				</td>
				<td>
					<div class="messageText">
						<h5 class="epsilon">Access denied</h5>
						<p class="zeta">Use of the requested functions is restricted.</p>
					</div>
				</td>
			</tr>
		</table>
	</div>
</div>
</body>
</html>