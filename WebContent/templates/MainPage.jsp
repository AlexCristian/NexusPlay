<%@ page language="java" contentType="text/html; charset=utf-8"
    pageEncoding="utf-8"%>
<div id="recentlyAdded">
	<%@ include file="/templates/elements/MediaDisplayer.jsp" %>
	 <script type="text/javascript">
	 $(document).ready(function(){
	 	$("#recentlyAdded > .categoriesHolder").prepend("<div class='categorySelector beta'>Recently added</div>");
	 });
	 </script>
</div>
<hr class="featurette-divider">
<div class="featurette">
	<!-- <img class="featurette-image pull-right" src="../assets/img/examples/browser-icon-chrome.png"> -->
	<h2 class='featurette-heading'>Nexus Play<span class="muted">, the open source web media center.</span></h2>
	<p class="lead">Easily access your media anytime, anywhere. Taking advantage of the newest web technologies, Nexus Play offers a lightweight, feature abundant media center. 
	At last, you can host your own personal YouTube, with the benefits that open source software provides.</p>
</div>